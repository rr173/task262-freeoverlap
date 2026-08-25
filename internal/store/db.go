// Package store 提供 SQLite 持久化层：建表迁移、批次/窗口/样本/边/快照的 CRUD。
//
// 使用纯 Go 驱动 modernc.org/sqlite（CGO 无关，离线可构建）。所有写入走
// 单连接串行化，避免 SQLite 写锁竞争；涉及状态机流转的更新都在事务内
// 先读后写，保证并发安全。
package store

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// Store 持有 SQLite 连接与迁移版本。
type Store struct {
	db   *sql.DB
	mu   sync.Mutex // 串行化写事务
	path string
}

// Open 打开（或创建）SQLite 数据库并执行迁移。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 单写者
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭底层数据库连接。
func (s *Store) Close() error { return s.db.Close() }

// Path 返回数据库文件路径。
func (s *Store) Path() string { return s.path }

// migrate 执行幂等建表迁移。
func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS calc_batches (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			method TEXT NOT NULL,
			temperature REAL NOT NULL,
			kt REAL NOT NULL,
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sampling_windows (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL REFERENCES calc_batches(id),
			label TEXT NOT NULL,
			center REAL NOT NULL,
			spring_const REAL NOT NULL,
			bias_version INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			sample_count INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS energy_samples (
			id TEXT PRIMARY KEY,
			window_id TEXT NOT NULL REFERENCES sampling_windows(id),
			seq INTEGER NOT NULL,
			energy REAL NOT NULL,
			bias REAL NOT NULL,
			weight REAL NOT NULL DEFAULT 1.0,
			content_hash TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS window_edges (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL REFERENCES calc_batches(id),
			lower_window_id TEXT NOT NULL,
			upper_window_id TEXT NOT NULL,
			overlap REAL NOT NULL,
			status TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			UNIQUE(lower_window_id, upper_window_id)
		)`,
		`CREATE TABLE IF NOT EXISTS reliability_snapshots (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL REFERENCES calc_batches(id),
			label TEXT NOT NULL,
			status TEXT NOT NULL,
			snapshot TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			frozen_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_windows_batch ON sampling_windows(batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_samples_window ON energy_samples(window_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_batch ON window_edges(batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_batch ON reliability_snapshots(batch_id)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
