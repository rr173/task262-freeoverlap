package store

import (
	"fmt"

	"task262-freeoverlap/internal/model"
)

// --- 边 ---

// UpsertEdge 插入或更新一条相邻窗口边（按 lower/upper 唯一键）。
func (s *Store) UpsertEdge(e *model.WindowEdge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO window_edges (id, batch_id, lower_window_id, upper_window_id, overlap, status, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(lower_window_id, upper_window_id) DO UPDATE SET
		   overlap = excluded.overlap, status = excluded.status, note = excluded.note`,
		e.ID, e.BatchID, e.LowerWindowID, e.UpperWindowID, e.Overlap, string(e.Status), e.Note, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert edge: %w", err)
	}
	return nil
}

// ReplaceEdges reconciles the persisted edge view with the latest diagnosis.
// Edges involving excluded windows must disappear instead of surviving as
// stale records after a rerun.
func (s *Store) ReplaceEdges(batchID string, edges []*model.WindowEdge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM window_edges WHERE batch_id = ?`, batchID); err != nil {
		return rollback(err)
	}
	for _, e := range edges {
		if _, err := tx.Exec(
			`INSERT INTO window_edges (id, batch_id, lower_window_id, upper_window_id, overlap, status, note, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.BatchID, e.LowerWindowID, e.UpperWindowID, e.Overlap, string(e.Status), e.Note, e.CreatedAt,
		); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// ListEdges 列出批次下的全部边（按 lower 窗口中心排序）。
func (s *Store) ListEdges(batchID string) ([]*model.WindowEdge, error) {
	rows, err := s.db.Query(
		`SELECT id, batch_id, lower_window_id, upper_window_id, overlap, status, note, created_at
		 FROM window_edges WHERE batch_id = ? ORDER BY lower_window_id ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.WindowEdge
	for rows.Next() {
		var e model.WindowEdge
		var status string
		if err := rows.Scan(&e.ID, &e.BatchID, &e.LowerWindowID, &e.UpperWindowID, &e.Overlap, &status, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Status = model.EdgeStatus(status)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// UpdateEdgeStatus 更新一条边的状态与备注（裁决）。
func (s *Store) UpdateEdgeStatus(id string, status model.EdgeStatus, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`UPDATE window_edges SET status = ?, note = ? WHERE id = ?`,
		string(status), note, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.E(model.ErrNotFound, "edge %s not found", id)
	}
	return nil
}
