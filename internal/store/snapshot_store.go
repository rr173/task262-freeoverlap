package store

import (
	"database/sql"
	"fmt"

	"task262-freeoverlap/internal/model"
)

// --- 快照 ---

// CreateSnapshot 插入一个可靠性快照。
func (s *Store) CreateSnapshot(sn *model.ReliabilitySnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO reliability_snapshots (id, batch_id, label, status, snapshot, created_at, frozen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sn.ID, sn.BatchID, sn.Label, string(sn.Status), sn.Snapshot, sn.CreatedAt, sn.FrozenAt,
	)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	return nil
}

// GetSnapshot 按 ID 读取快照。
func (s *Store) GetSnapshot(id string) (*model.ReliabilitySnapshot, error) {
	row := s.db.QueryRow(
		`SELECT id, batch_id, label, status, snapshot, created_at, frozen_at
		 FROM reliability_snapshots WHERE id = ?`, id)
	var sn model.ReliabilitySnapshot
	var status string
	if err := row.Scan(&sn.ID, &sn.BatchID, &sn.Label, &status, &sn.Snapshot, &sn.CreatedAt, &sn.FrozenAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, model.E(model.ErrNotFound, "snapshot %s not found", id)
		}
		return nil, err
	}
	sn.Status = model.SnapshotStatus(status)
	return &sn, nil
}

// ListSnapshots 列出批次下的快照（按创建时间倒序）。
func (s *Store) ListSnapshots(batchID string) ([]*model.ReliabilitySnapshot, error) {
	rows, err := s.db.Query(
		`SELECT id, batch_id, label, status, snapshot, created_at, frozen_at
		 FROM reliability_snapshots WHERE batch_id = ? ORDER BY created_at DESC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ReliabilitySnapshot
	for rows.Next() {
		var sn model.ReliabilitySnapshot
		var status string
		if err := rows.Scan(&sn.ID, &sn.BatchID, &sn.Label, &status, &sn.Snapshot, &sn.CreatedAt, &sn.FrozenAt); err != nil {
			return nil, err
		}
		sn.Status = model.SnapshotStatus(status)
		out = append(out, &sn)
	}
	return out, rows.Err()
}

// UpdateSnapshotStatus 更新快照状态（发布/替代）。
func (s *Store) UpdateSnapshotStatus(id string, status model.SnapshotStatus, frozenAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`UPDATE reliability_snapshots SET status = ?, frozen_at = ? WHERE id = ?`,
		string(status), frozenAt, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.E(model.ErrNotFound, "snapshot %s not found", id)
	}
	return nil
}

// CommitSnapshotPublication atomically freezes a draft and seals its batch.
// The conditional snapshot update guarantees that concurrent publishers can
// produce at most one successful publication.
func (s *Store) CommitSnapshotPublication(id, batchID string, frozenAt int64) error {
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
	res, err := tx.Exec(
		`UPDATE reliability_snapshots SET status = ?, frozen_at = ?
		 WHERE id = ? AND batch_id = ? AND status = ?`,
		string(model.SnapshotPublished), frozenAt, id, batchID, string(model.SnapshotDraft))
	if err != nil {
		return rollback(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return rollback(err)
	}
	if n != 1 {
		return rollback(model.E(model.ErrConflict, "snapshot %s is no longer draft", id))
	}
	res, err = tx.Exec(
		`UPDATE calc_batches SET status = ?, updated_at = ? WHERE id = ?`,
		string(model.BatchSealed), model.NowMillis(), batchID)
	if err != nil {
		return rollback(err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return rollback(err)
	}
	if n != 1 {
		return rollback(model.E(model.ErrNotFound, "batch %s not found", batchID))
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
