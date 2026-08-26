package store

import (
	"math"

	"task262-freeoverlap/internal/model"
)

// --- 样本 ---

// InsertSamples 原子导入一批能量样本，并同步窗口样本计数。
// content_hash 唯一冲突按幂等重复处理；任何其他错误都会回滚整批请求。
//
// 任一样本的 energy 或 bias 为 NaN/Inf 都会让重加权不可解，因此在开
// 启事务之前即整批拒绝——非法观测绝不落库，窗口计数保持导入前状态。
func (s *Store) InsertSamples(windowID string, samples []*model.EnergySample) (int, error) {
	for _, sm := range samples {
		if math.IsNaN(sm.Energy) || math.IsInf(sm.Energy, 0) ||
			math.IsNaN(sm.Bias) || math.IsInf(sm.Bias, 0) {
			return 0, model.E(model.ErrInvalid, "sample %s has non-finite energy/bias", sm.ID)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	rollback := func(err error) (int, error) {
		_ = tx.Rollback()
		return 0, err
	}
	inserted := 0
	for _, sm := range samples {
		res, err := tx.Exec(
			`INSERT OR IGNORE INTO energy_samples (id, window_id, seq, energy, bias, weight, content_hash, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sm.ID, sm.WindowID, sm.Seq, sm.Energy, sm.Bias, sm.Weight, sm.ContentHash, sm.CreatedAt)
		if err != nil {
			return rollback(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return rollback(err)
		}
		inserted += int(n)
	}
	res, err := tx.Exec(
		`UPDATE sampling_windows
		 SET sample_count = (SELECT COUNT(*) FROM energy_samples WHERE window_id = ?), updated_at = ?
		 WHERE id = ?`, windowID, model.NowMillis(), windowID)
	if err != nil {
		return rollback(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return rollback(err)
	}
	if n == 0 {
		return rollback(model.E(model.ErrNotFound, "window %s not found", windowID))
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// ListSamples 列出窗口内的全部样本（按 seq 升序）。
func (s *Store) ListSamples(windowID string) ([]*model.EnergySample, error) {
	rows, err := s.db.Query(
		`SELECT id, window_id, seq, energy, bias, weight, content_hash, created_at
		 FROM energy_samples WHERE window_id = ? ORDER BY seq ASC`, windowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.EnergySample
	for rows.Next() {
		var sm model.EnergySample
		if err := rows.Scan(&sm.ID, &sm.WindowID, &sm.Seq, &sm.Energy, &sm.Bias, &sm.Weight, &sm.ContentHash, &sm.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &sm)
	}
	return out, rows.Err()
}

// UpdateSampleWeight 批量更新权重（偏置校正后回写）。
func (s *Store) UpdateSampleWeight(id string, weight float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE energy_samples SET weight = ? WHERE id = ?`, weight, id)
	return err
}
