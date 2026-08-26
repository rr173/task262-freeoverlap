package store

import (
	"database/sql"

	"task262-freeoverlap/internal/model"
)

// --- 样本 ---

// InsertSamples 原子导入一批能量样本，并同步窗口样本计数。
// content_hash 唯一冲突按幂等重复处理；任何其他错误都会回滚整批请求。
// 封存后的批次只读：在写事务内原子校验窗口所属批次未封存，杜绝并发发布
// 在校验与写入之间插入封存（TOCTOU）的窗口。
func (s *Store) InsertSamples(windowID string, samples []*model.EnergySample) (int, error) {
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
	batchID, err := s.windowBatchID(tx, windowID)
	if err != nil {
		return rollback(err)
	}
	if err := s.sealedBatchErr(tx, batchID); err != nil {
		return rollback(err)
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
// 封存后的批次只读：拒绝改写已封存批次窗口的样本权重。
func (s *Store) UpdateSampleWeight(id string, weight float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batchID, err := s.sampleBatchID(s.db, id)
	if err != nil {
		return err
	}
	if err := s.sealedBatchErr(s.db, batchID); err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE energy_samples SET weight = ? WHERE id = ?`, weight, id)
	return err
}

// sampleBatchID resolves the owning batch of a sample via its window so the
// seal check can guard sample mutations. A missing sample surfaces as
// ErrNotFound.
func (s *Store) sampleBatchID(runner execRunner, sampleID string) (string, error) {
	var batchID string
	err := runner.QueryRow(
		`SELECT w.batch_id
		 FROM energy_samples es JOIN sampling_windows w ON w.id = es.window_id
		 WHERE es.id = ?`, sampleID).Scan(&batchID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", model.E(model.ErrNotFound, "sample %s not found", sampleID)
		}
		return "", err
	}
	return batchID, nil
}
