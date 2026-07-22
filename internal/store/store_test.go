package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesExistingSeedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workseed.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE seeds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			parent_id INTEGER REFERENCES seeds(id) ON DELETE SET NULL,
			type TEXT NOT NULL CHECK (type IN ('idea', 'feature', 'todo', 'bug')),
			status TEXT NOT NULL DEFAULT 'inbox' CHECK (status IN ('inbox', 'done')),
			title TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT 'middle' CHECK (priority IN ('high', 'middle', 'low')),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at TEXT
		);
		CREATE INDEX idx_seeds_project_type_status ON seeds(project_id, type, status, updated_at DESC);
		INSERT INTO projects(id, name) VALUES(1, '已有项目');
		INSERT INTO seeds(project_id, type, status, title, priority, completed_at)
		VALUES(1, 'todo', 'done', '已有种子', 'high', '2026-07-21 08:00:00');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var status, title, completedAt string
	var startedAt, durationSeconds any
	if err := db.QueryRow(`SELECT status, title, started_at, completed_at, duration_seconds FROM seeds WHERE id=1`).
		Scan(&status, &title, &startedAt, &completedAt, &durationSeconds); err != nil {
		t.Fatal(err)
	}
	if status != "done" || title != "已有种子" || completedAt != "2026-07-21 08:00:00" {
		t.Fatalf("migrated seed = status %q, title %q, completedAt %q", status, title, completedAt)
	}
	if startedAt != nil || durationSeconds != nil {
		t.Fatalf("new timing fields should be empty after migration: startedAt=%v duration=%v", startedAt, durationSeconds)
	}
	if _, err := db.Exec(`UPDATE seeds SET status='doing' WHERE id=1`); err != nil {
		t.Fatalf("doing status is rejected after migration: %v", err)
	}
	if _, err := db.Exec(`UPDATE seeds SET status='paused' WHERE id=1`); err != nil {
		t.Fatalf("paused status is rejected after migration: %v", err)
	}
	if _, err := db.Exec(`UPDATE seeds SET status='skipped' WHERE id=1`); err != nil {
		t.Fatalf("skipped status is rejected after migration: %v", err)
	}
	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}
