package store

import (
	"database/sql"
	"fmt"

	"task262-freeoverlap/internal/model"
)

// --- 批次 ---

// CreateBatch 插入一个新批次。
func (s *Store) CreateBatch(b *model.CalcBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO calc_batches (id, name, method, temperature, kt, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Name, b.Method, b.Temperature, b.KT, string(b.Status), b.CreatedAt, b.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert batch: %w", err)
	}
	return nil
}

// GetBatch 按 ID 读取批次。
func (s *Store) GetBatch(id string) (*model.CalcBatch, error) {
	row := s.db.QueryRow(
		`SELECT id, name, method, temperature, kt, status, created_at, updated_at
		 FROM calc_batches WHERE id = ?`, id)
	var b model.CalcBatch
	var status string
	if err := row.Scan(&b.ID, &b.Name, &b.Method, &b.Temperature, &b.KT, &status, &b.CreatedAt, &b.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, model.E(model.ErrNotFound, "batch %s not found", id)
		}
		return nil, err
	}
	b.Status = model.BatchStatus(status)
	return &b, nil
}

// ListBatches 列出全部批次（按创建时间倒序）。
func (s *Store) ListBatches() ([]*model.CalcBatch, error) {
	rows, err := s.db.Query(
		`SELECT id, name, method, temperature, kt, status, created_at, updated_at
		 FROM calc_batches ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.CalcBatch
	for rows.Next() {
		var b model.CalcBatch
		var status string
		if err := rows.Scan(&b.ID, &b.Name, &b.Method, &b.Temperature, &b.KT, &status, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		b.Status = model.BatchStatus(status)
		out = append(out, &b)
	}
	return out, rows.Err()
}

// UpdateBatchStatus 更新批次状态并刷新 updated_at。
func (s *Store) UpdateBatchStatus(id string, status model.BatchStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`UPDATE calc_batches SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), model.NowMillis(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.E(model.ErrNotFound, "batch %s not found", id)
	}
	return nil
}
