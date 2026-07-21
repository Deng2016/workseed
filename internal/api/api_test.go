package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"workseed/internal/store"
)

func TestSeedStatusTimestampsAndDuration(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('测试项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	Register(mux, db)

	create := func(title string) seed {
		t.Helper()
		return seedRequest(t, mux, http.MethodPost, "/api/seeds", seed{
			ProjectID: projectID,
			Type:      "todo",
			Status:    "inbox",
			Title:     title,
			Priority:  "middle",
		}, http.StatusCreated)
	}
	patchStatus := func(item seed, status string) seed {
		t.Helper()
		item.Status = status
		return seedRequest(t, mux, http.MethodPatch, "/api/seeds/"+itoa(item.ID), item, http.StatusOK)
	}

	direct := patchStatus(create("直接完成"), "done")
	if direct.StartedAt != nil {
		t.Fatalf("direct completion unexpectedly recorded a start time: %v", *direct.StartedAt)
	}
	if direct.CompletedAt == nil {
		t.Fatal("direct completion did not record a completion time")
	}
	if direct.DurationSec != nil {
		t.Fatalf("direct completion unexpectedly calculated duration: %d", *direct.DurationSec)
	}

	stepByStep := patchStatus(create("逐步完成"), "doing")
	if stepByStep.StartedAt == nil {
		t.Fatal("entering doing did not record a start time")
	}
	if stepByStep.CompletedAt != nil || stepByStep.DurationSec != nil {
		t.Fatal("entering doing unexpectedly recorded completion data")
	}

	if _, err := db.Exec(`UPDATE seeds SET started_at=datetime(CURRENT_TIMESTAMP, '-3661 seconds') WHERE id=?`, stepByStep.ID); err != nil {
		t.Fatal(err)
	}
	stepByStep = patchStatus(stepByStep, "done")
	if stepByStep.CompletedAt == nil {
		t.Fatal("entering done did not record a completion time")
	}
	if stepByStep.DurationSec == nil || *stepByStep.DurationSec != 3661 {
		t.Fatalf("duration = %v, want 3661 seconds", stepByStep.DurationSec)
	}
}

func TestDoingStatusCanBeCreatedAndFiltered(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('测试项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()

	mux := http.NewServeMux()
	Register(mux, db)
	created := seedRequest(t, mux, http.MethodPost, "/api/seeds", seed{
		ProjectID: projectID,
		Type:      "feature",
		Status:    "doing",
		Title:     "进行中的功能",
		Priority:  "high",
	}, http.StatusCreated)
	if created.StartedAt == nil {
		t.Fatal("creating a doing seed did not record a start time")
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/seeds?projectId="+itoa(projectID)+"&status=doing", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Seed-Count-Doing"); got != "1" {
		t.Fatalf("X-Seed-Count-Doing = %q, want 1", got)
	}
	var items []seed
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != "doing" {
		t.Fatalf("filtered items = %#v", items)
	}
}

func TestSeedsAreOrderedByCreationTimeDescending(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('排序项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	_, err = db.Exec(`INSERT INTO seeds(project_id, type, status, title, priority, created_at, updated_at) VALUES
		(?, 'todo', 'inbox', '较早创建', 'high', '2026-01-01 00:00:00', CURRENT_TIMESTAMP),
		(?, 'todo', 'done', '较晚创建', 'low', '2026-02-01 00:00:00', '2026-02-01 00:00:00')`, projectID, projectID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	Register(mux, db)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/seeds?projectId="+itoa(projectID)+"&status=all", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var items []seed
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "较晚创建" || items[1].Title != "较早创建" {
		t.Fatalf("items are not ordered by createdAt descending: %#v", items)
	}
}

func seedRequest(t *testing.T, handler http.Handler, method, path string, input seed, wantStatus int) seed {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body = %s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	var output seed
	if err := json.NewDecoder(recorder.Body).Decode(&output); err != nil {
		t.Fatal(err)
	}
	return output
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}
