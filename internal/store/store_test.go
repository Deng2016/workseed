package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	var startedAt, durationSeconds, claimToken any
	if err := db.QueryRow(`SELECT status, title, started_at, completed_at, duration_seconds, claim_token FROM seeds WHERE id=1`).
		Scan(&status, &title, &startedAt, &completedAt, &durationSeconds, &claimToken); err != nil {
		t.Fatal(err)
	}
	if status != "done" || title != "已有种子" || completedAt != "2026-07-21T08:00:00Z" {
		t.Fatalf("migrated seed = status %q, title %q, completedAt %q", status, title, completedAt)
	}
	if startedAt != nil || durationSeconds != nil {
		t.Fatalf("new timing fields should be empty after migration: startedAt=%v duration=%v", startedAt, durationSeconds)
	}
	if claimToken != nil {
		t.Fatalf("claim token should be empty after migration: %v", claimToken)
	}
	var archivedAt any
	if err := db.QueryRow(`SELECT archived_at FROM projects WHERE id=1`).Scan(&archivedAt); err != nil {
		t.Fatal(err)
	}
	if archivedAt != nil {
		t.Fatalf("migrated project unexpectedly archived: %v", archivedAt)
	}
	var workdayStart, workdayEnd string
	if err := db.QueryRow(`SELECT workday_start, workday_end FROM app_settings WHERE id=1`).Scan(&workdayStart, &workdayEnd); err != nil {
		t.Fatal(err)
	}
	if workdayStart != "10:00" || workdayEnd != "19:00" {
		t.Fatalf("default workday = %s-%s, want 10:00-19:00", workdayStart, workdayEnd)
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

func TestOpenNormalizesAllLegacyTimestampsWithoutChangingSeedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workseed.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			archived_at TEXT
		);
		CREATE TABLE app_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			workday_start TEXT NOT NULL DEFAULT '10:00',
			workday_end TEXT NOT NULL DEFAULT '19:00',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
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
			duration_seconds INTEGER CHECK (duration_seconds IS NULL OR duration_seconds >= 0)
		);
		CREATE INDEX idx_seeds_project_type_status ON seeds(project_id, type, status, updated_at DESC);
		INSERT INTO projects(id, name, created_at, updated_at, archived_at)
		VALUES(1, '当前项目', '2026-07-20 08:00:00', '2026-07-21 08:00:00', '2026-07-22 08:00:00');
		INSERT INTO app_settings(id, workday_start, workday_end, updated_at)
		VALUES(1, '09:00', '18:00', '2026-07-20 09:00:00');
		INSERT INTO seeds(project_id, type, status, title, priority, created_at, updated_at, started_at, completed_at, duration_seconds)
		VALUES(1, 'todo', 'done', '已经完成', 'high', '2026-07-20 10:00:00', '2026-07-22 10:00:00', '2026-07-21 10:00:00', '2026-07-22 10:00:00', 321);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}

	var status, createdAt, updatedAt, startedAt, completedAt string
	var duration int64
	var claimToken any
	if err := db.QueryRow(`SELECT status, created_at, updated_at, started_at, completed_at, duration_seconds, claim_token FROM seeds WHERE id=1`).
		Scan(&status, &createdAt, &updatedAt, &startedAt, &completedAt, &duration, &claimToken); err != nil {
		t.Fatal(err)
	}
	if status != "done" || createdAt != "2026-07-20T10:00:00Z" || updatedAt != "2026-07-22T10:00:00Z" ||
		startedAt != "2026-07-21T10:00:00Z" || completedAt != "2026-07-22T10:00:00Z" || duration != 321 || claimToken != nil {
		t.Fatalf("seed changed during timestamp migration: status=%q createdAt=%q updatedAt=%q startedAt=%q completedAt=%q duration=%d claimToken=%v",
			status, createdAt, updatedAt, startedAt, completedAt, duration, claimToken)
	}
	var projectCreatedAt, projectUpdatedAt, projectArchivedAt, settingsUpdatedAt string
	if err := db.QueryRow(`SELECT created_at, updated_at, archived_at FROM projects WHERE id=1`).
		Scan(&projectCreatedAt, &projectUpdatedAt, &projectArchivedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT updated_at FROM app_settings WHERE id=1`).Scan(&settingsUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if projectCreatedAt != "2026-07-20T08:00:00Z" || projectUpdatedAt != "2026-07-21T08:00:00Z" ||
		projectArchivedAt != "2026-07-22T08:00:00Z" || settingsUpdatedAt != "2026-07-20T09:00:00Z" {
		t.Fatalf("migrated timestamps = project(%q, %q, %q), settings(%q)",
			projectCreatedAt, projectUpdatedAt, projectArchivedAt, settingsUpdatedAt)
	}
	var invalidReferenceTable string
	if err := db.QueryRow(`PRAGMA foreign_key_check`).Scan(&invalidReferenceTable); err != sql.ErrNoRows {
		t.Fatalf("foreign key check returned table %q, error %v", invalidReferenceTable, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var reopenedStartedAt, reopenedProjectArchivedAt string
	if err := db.QueryRow(`SELECT started_at FROM seeds WHERE id=1`).Scan(&reopenedStartedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT archived_at FROM projects WHERE id=1`).Scan(&reopenedProjectArchivedAt); err != nil {
		t.Fatal(err)
	}
	if reopenedStartedAt != startedAt {
		t.Fatalf("idempotent migration changed startedAt from %q to %q", startedAt, reopenedStartedAt)
	}
	if reopenedProjectArchivedAt != projectArchivedAt {
		t.Fatalf("idempotent migration changed archivedAt from %q to %q", projectArchivedAt, reopenedProjectArchivedAt)
	}
}

func TestNewSchemaDefaultsUseRFC3339UTC(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projectResult, err := db.Exec(`INSERT INTO projects(name) VALUES('UTC 项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := projectResult.LastInsertId()
	if _, err := db.Exec(`INSERT INTO seeds(project_id, type, title) VALUES(?, 'todo', 'UTC 事种')`, projectID); err != nil {
		t.Fatal(err)
	}

	var projectCreatedAt, projectUpdatedAt, settingsUpdatedAt, seedCreatedAt, seedUpdatedAt string
	if err := db.QueryRow(`SELECT created_at, updated_at FROM projects WHERE id=?`, projectID).
		Scan(&projectCreatedAt, &projectUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT updated_at FROM app_settings WHERE id=1`).Scan(&settingsUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT created_at, updated_at FROM seeds WHERE project_id=?`, projectID).
		Scan(&seedCreatedAt, &seedUpdatedAt); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{projectCreatedAt, projectUpdatedAt, settingsUpdatedAt, seedCreatedAt, seedUpdatedAt} {
		assertRFC3339UTC(t, value)
	}
}

func assertRFC3339UTC(t *testing.T, value string) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("timestamp %q is not RFC 3339: %v", value, err)
	}
	if !strings.HasSuffix(value, "Z") || parsed.Location() != time.UTC {
		t.Fatalf("timestamp %q is not explicit UTC", value)
	}
}
