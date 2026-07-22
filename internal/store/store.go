package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize schema: %w", err)
	}
	if err := migrateSeedSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate seed schema: %w", err)
	}
	return db, nil
}

func migrateSeedSchema(db *sql.DB) error {
	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'seeds'`).Scan(&definition); err != nil {
		return err
	}
	isCurrentSchema := strings.Contains(definition, "priority TEXT") &&
		strings.Contains(definition, "'doing'") &&
		strings.Contains(definition, "'paused'") &&
		strings.Contains(definition, "'skipped'") &&
		strings.Contains(definition, "started_at TEXT") &&
		strings.Contains(definition, "duration_seconds INTEGER") &&
		!strings.Contains(definition, "'planned'") &&
		!strings.Contains(definition, "'someday'") &&
		!strings.Contains(definition, "'archived'")
	if isCurrentSchema && strings.Contains(definition, "claim_token TEXT") {
		return nil
	}
	if isCurrentSchema {
		_, err := db.Exec(`ALTER TABLE seeds ADD COLUMN claim_token TEXT`)
		return err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys=ON`)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(seedSchemaMigration); err != nil {
		return err
	}
	return tx.Commit()
}

const seedSchemaMigration = `
DROP INDEX IF EXISTS idx_seeds_project_type_status;
ALTER TABLE seeds RENAME TO seeds_before_schema_migration;

CREATE TABLE seeds (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  parent_id INTEGER REFERENCES seeds(id) ON DELETE SET NULL,
  type TEXT NOT NULL CHECK (type IN ('idea', 'feature', 'todo', 'bug')),
  status TEXT NOT NULL DEFAULT 'inbox' CHECK (status IN ('inbox', 'doing', 'paused', 'skipped', 'done')),
  title TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  priority TEXT NOT NULL DEFAULT 'middle' CHECK (priority IN ('high', 'middle', 'low')),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at TEXT,
  completed_at TEXT,
  duration_seconds INTEGER CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
  claim_token TEXT
);

INSERT INTO seeds(id, project_id, parent_id, type, status, title, content, priority, created_at, updated_at, started_at, completed_at, duration_seconds, claim_token)
SELECT
  id, project_id, parent_id, type,
  CASE WHEN status IN ('doing', 'paused', 'skipped', 'done') THEN status ELSE 'inbox' END,
  title, content,
  CASE
    WHEN status IN ('archived', 'someday') THEN 'low'
    WHEN CAST(priority AS TEXT) IN ('high', 'middle', 'low') THEN CAST(priority AS TEXT)
    WHEN CAST(priority AS INTEGER) >= 3 THEN 'high'
    WHEN CAST(priority AS INTEGER) = 1 THEN 'low'
    ELSE 'middle'
  END,
  created_at, updated_at, NULL, completed_at, NULL, NULL
FROM seeds_before_schema_migration;

DROP TABLE seeds_before_schema_migration;
CREATE INDEX idx_seeds_project_type_status
ON seeds(project_id, type, status, updated_at DESC);
`

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_projects_name
ON projects(name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS seeds (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  parent_id INTEGER REFERENCES seeds(id) ON DELETE SET NULL,
  type TEXT NOT NULL CHECK (type IN ('idea', 'feature', 'todo', 'bug')),
  status TEXT NOT NULL DEFAULT 'inbox' CHECK (status IN ('inbox', 'doing', 'paused', 'skipped', 'done')),
  title TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  priority TEXT NOT NULL DEFAULT 'middle' CHECK (priority IN ('high', 'middle', 'low')),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at TEXT,
  completed_at TEXT,
  duration_seconds INTEGER CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
  claim_token TEXT
);

CREATE INDEX IF NOT EXISTS idx_seeds_project_type_status
ON seeds(project_id, type, status, updated_at DESC);
`
