package store

import (
	"database/sql"
	"fmt"

	"task262-freeoverlap/internal/model"
)

// --- 窗口 ---

// CreateWindow 插入一个采样窗口。
// 封存后的批次只读：拒绝向已封存批次追加窗口。
func (s *Store) CreateWindow(w *model.SamplingWindow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sealedBatchErr(s.db, w.BatchID); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO sampling_windows (id, batch_id, label, center, spring_const, bias_version, status, sample_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.BatchID, w.Label, w.Center, w.SpringConst, w.BiasVersion, string(w.Status), w.SampleCount, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert window: %w", err)
	}
	return nil
}

// GetWindow 按 ID 读取窗口。
func (s *Store) GetWindow(id string) (*model.SamplingWindow, error) {
	row := s.db.QueryRow(
		`SELECT id, batch_id, label, center, spring_const, bias_version, status, sample_count, created_at, updated_at
		 FROM sampling_windows WHERE id = ?`, id)
	var w model.SamplingWindow
	var status string
	if err := row.Scan(&w.ID, &w.BatchID, &w.Label, &w.Center, &w.SpringConst, &w.BiasVersion, &status, &w.SampleCount, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, model.E(model.ErrNotFound, "window %s not found", id)
		}
		return nil, err
	}
	w.Status = model.WindowStatus(status)
	return &w, nil
}

// ListWindows 列出批次下的全部窗口（按中心坐标升序）。
func (s *Store) ListWindows(batchID string) ([]*model.SamplingWindow, error) {
	rows, err := s.db.Query(
		`SELECT id, batch_id, label, center, spring_const, bias_version, status, sample_count, created_at, updated_at
		 FROM sampling_windows WHERE batch_id = ? ORDER BY center ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.SamplingWindow
	for rows.Next() {
		var w model.SamplingWindow
		var status string
		if err := rows.Scan(&w.ID, &w.BatchID, &w.Label, &w.Center, &w.SpringConst, &w.BiasVersion, &status, &w.SampleCount, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.Status = model.WindowStatus(status)
		out = append(out, &w)
	}
	return out, rows.Err()
}

// UpdateWindowStatus 更新窗口状态与样本数。
// 封存后的批次只读：拒绝修改已封存批次的窗口。
func (s *Store) UpdateWindowStatus(id string, status model.WindowStatus, sampleCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batchID, err := s.windowBatchID(s.db, id)
	if err != nil {
		return err
	}
	if err := s.sealedBatchErr(s.db, batchID); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE sampling_windows SET status = ?, sample_count = ?, updated_at = ? WHERE id = ?`,
		string(status), sampleCount, model.NowMillis(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.E(model.ErrNotFound, "window %s not found", id)
	}
	return nil
}

// CountSamples 统计窗口内的样本数。
func (s *Store) CountSamples(windowID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM energy_samples WHERE window_id = ?`, windowID).Scan(&n)
	return n, err
}

// windowBatchID resolves the owning batch of a window on a given runner so the
// seal check can run inside the caller's write transaction (atomic with the
// mutation). A missing window surfaces as ErrNotFound.
func (s *Store) windowBatchID(runner execRunner, windowID string) (string, error) {
	var batchID string
	err := runner.QueryRow(
		`SELECT batch_id FROM sampling_windows WHERE id = ?`, windowID).Scan(&batchID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", model.E(model.ErrNotFound, "window %s not found", windowID)
		}
		return "", err
	}
	return batchID, nil
}
