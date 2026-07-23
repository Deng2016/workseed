package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"workseed/internal/store"
	"workseed/internal/worktime"
)

func TestSeedStatusTimestampsAndDuration(t *testing.T) {
	testStartedAt := time.Now().UTC().Add(-2 * time.Second)
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
	assertRFC3339UTC(t, direct.CreatedAt)
	assertRFC3339UTC(t, direct.UpdatedAt)
	if direct.StartedAt != nil {
		t.Fatalf("direct completion unexpectedly recorded a start time: %v", *direct.StartedAt)
	}
	if direct.CompletedAt == nil {
		t.Fatal("direct completion did not record a completion time")
	}
	assertRFC3339UTC(t, *direct.CompletedAt)
	if direct.DurationSec != nil {
		t.Fatalf("direct completion unexpectedly calculated duration: %d", *direct.DurationSec)
	}

	stepByStep := patchStatus(create("逐步完成"), "doing")
	if stepByStep.StartedAt == nil {
		t.Fatal("entering doing did not record a start time")
	}
	assertRFC3339UTC(t, *stepByStep.StartedAt)
	if stepByStep.CompletedAt != nil || stepByStep.DurationSec != nil {
		t.Fatal("entering doing unexpectedly recorded completion data")
	}
	if _, err := db.Exec(`UPDATE seeds SET claim_token='agent-claim' WHERE id=?`, stepByStep.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`UPDATE seeds SET started_at=strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-3661 seconds') WHERE id=?`, stepByStep.ID); err != nil {
		t.Fatal(err)
	}
	stepByStep = patchStatus(stepByStep, "done")
	if stepByStep.CompletedAt == nil {
		t.Fatal("entering done did not record a completion time")
	}
	assertRFC3339UTC(t, *stepByStep.CompletedAt)
	wantDuration, err := worktime.DurationSeconds(*stepByStep.StartedAt, *stepByStep.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	if stepByStep.DurationSec == nil || *stepByStep.DurationSec != wantDuration {
		t.Fatalf("duration = %v, want %d seconds", stepByStep.DurationSec, wantDuration)
	}
	var storedCreatedAt, storedUpdatedAt, storedCompletedAt string
	if err := db.QueryRow(`SELECT created_at, updated_at, completed_at FROM seeds WHERE id=?`, direct.ID).
		Scan(&storedCreatedAt, &storedUpdatedAt, &storedCompletedAt); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{storedCreatedAt, storedUpdatedAt, storedCompletedAt} {
		assertRFC3339UTC(t, value)
		stored, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatalf("database timestamp %q: %v", value, err)
		}
		if stored.Before(testStartedAt) || stored.After(time.Now().UTC().Add(2*time.Second)) {
			t.Fatalf("database timestamp %q is outside the UTC test window", value)
		}
	}
	var retainedClaimToken *string
	if err := db.QueryRow(`SELECT claim_token FROM seeds WHERE id=?`, stepByStep.ID).Scan(&retainedClaimToken); err != nil {
		t.Fatal(err)
	}
	if retainedClaimToken == nil || *retainedClaimToken != "agent-claim" {
		t.Fatalf("claim token was not retained when completing: %v", retainedClaimToken)
	}
	stepByStep = patchStatus(stepByStep, "inbox")
	if err := db.QueryRow(`SELECT claim_token FROM seeds WHERE id=?`, stepByStep.ID).Scan(&retainedClaimToken); err != nil {
		t.Fatal(err)
	}
	if retainedClaimToken != nil {
		t.Fatalf("claim token was not cleared when reopening: %v", *retainedClaimToken)
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

func TestPausedAndSkippedStatusesCanBeCreatedUpdatedAndFiltered(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('暂停跳过项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	mux := http.NewServeMux()
	Register(mux, db)

	paused := seedRequest(t, mux, http.MethodPost, "/api/seeds", seed{
		ProjectID: projectID,
		Type:      "feature",
		Status:    "paused",
		Title:     "暂停处理",
		Priority:  "middle",
	}, http.StatusCreated)
	if paused.Status != "paused" || paused.StartedAt != nil || paused.CompletedAt != nil {
		t.Fatalf("paused seed = %#v", paused)
	}

	skipped := seedRequest(t, mux, http.MethodPost, "/api/seeds", seed{
		ProjectID: projectID,
		Type:      "todo",
		Status:    "done",
		Title:     "稍后跳过",
		Priority:  "low",
	}, http.StatusCreated)
	if skipped.CompletedAt == nil {
		t.Fatal("done seed did not record completion time")
	}
	skipped.Status = "skipped"
	skipped = seedRequest(t, mux, http.MethodPatch, "/api/seeds/"+itoa(skipped.ID), skipped, http.StatusOK)
	if skipped.Status != "skipped" || skipped.CompletedAt != nil || skipped.DurationSec != nil {
		t.Fatalf("skipped seed = %#v", skipped)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/seeds?projectId="+itoa(projectID)+"&status=paused,skipped", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var items []seed
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("filtered items = %#v", items)
	}
	if got := recorder.Header().Get("X-Seed-Count-Paused"); got != "1" {
		t.Fatalf("X-Seed-Count-Paused = %q, want 1", got)
	}
	if got := recorder.Header().Get("X-Seed-Count-Skipped"); got != "1" {
		t.Fatalf("X-Seed-Count-Skipped = %q, want 1", got)
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
		(?, 'todo', 'inbox', '较早创建', 'high', '2026-01-01T00:00:00Z', strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
		(?, 'todo', 'done', '较晚创建', 'low', '2026-02-01T00:00:00Z', '2026-02-01T00:00:00Z')`, projectID, projectID)
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

func TestSeedsArePaginatedTwentyAtATimeByDefault(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('分页项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	for index := 1; index <= 25; index++ {
		if _, err := db.Exec(`INSERT INTO seeds(project_id, type, status, title, priority) VALUES(?, 'todo', 'inbox', ?, 'middle')`, projectID, "种子"+strconv.Itoa(index)); err != nil {
			t.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	Register(mux, db)
	requestPage := func(path string) (*httptest.ResponseRecorder, []seed) {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", path, recorder.Code, recorder.Body.String())
		}
		var items []seed
		if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
			t.Fatal(err)
		}
		return recorder, items
	}

	basePath := "/api/seeds?projectId=" + itoa(projectID)
	firstResponse, firstPage := requestPage(basePath)
	if len(firstPage) != 20 || firstPage[0].Title != "种子25" || firstPage[19].Title != "种子6" {
		t.Fatalf("first page = %#v", firstPage)
	}
	if got := firstResponse.Header().Get("X-Seed-Filtered-Total"); got != "25" {
		t.Fatalf("X-Seed-Filtered-Total = %q, want 25", got)
	}
	if got := firstResponse.Header().Get("X-Seed-Page-Size"); got != "20" {
		t.Fatalf("X-Seed-Page-Size = %q, want 20", got)
	}
	if got := firstResponse.Header().Get("X-Seed-Has-More"); got != "true" {
		t.Fatalf("X-Seed-Has-More = %q, want true", got)
	}

	secondResponse, secondPage := requestPage(basePath + "&page=2")
	if len(secondPage) != 5 || secondPage[0].Title != "种子5" || secondPage[4].Title != "种子1" {
		t.Fatalf("second page = %#v", secondPage)
	}
	if got := secondResponse.Header().Get("X-Seed-Page"); got != "2" {
		t.Fatalf("X-Seed-Page = %q, want 2", got)
	}
	if got := secondResponse.Header().Get("X-Seed-Has-More"); got != "false" {
		t.Fatalf("X-Seed-Has-More = %q, want false", got)
	}
}

func TestSeedsRejectInvalidPagination(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.Exec(`INSERT INTO projects(name) VALUES('分页参数项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	mux := http.NewServeMux()
	Register(mux, db)

	for _, query := range []string{"page=0", "page=nope", "pageSize=0", "pageSize=101"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/seeds?projectId="+itoa(projectID)+"&"+query, nil)
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400; body = %s", query, recorder.Code, recorder.Body.String())
		}
	}
}

func TestSeedsSupportMultipleAndEmptyFilters(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('多选筛选项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	_, err = db.Exec(`INSERT INTO seeds(project_id, type, status, title, priority) VALUES
		(?, 'idea', 'inbox', '灵感待实现', 'high'),
		(?, 'feature', 'doing', '功能进行中', 'middle'),
		(?, 'todo', 'done', '事项已完成', 'low'),
		(?, 'bug', 'inbox', '缺陷待实现', 'low')`, projectID, projectID, projectID, projectID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	Register(mux, db)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/seeds?projectId="+itoa(projectID)+"&type=idea&type=feature&status=inbox,doing&priority=high&priority=middle", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var items []seed
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "功能进行中" || items[1].Title != "灵感待实现" {
		t.Fatalf("filtered items = %#v", items)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/seeds?projectId="+itoa(projectID)+"&type=&status=inbox", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("empty filter status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("empty type selection returned %d items", len(items))
	}
}

func TestSeedsCanBeListedAcrossProjectsWithoutProjectID(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	firstResult, err := db.Exec(`INSERT INTO projects(name) VALUES('跨项目一')`)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := db.Exec(`INSERT INTO projects(name) VALUES('跨项目二')`)
	if err != nil {
		t.Fatal(err)
	}
	firstProjectID, _ := firstResult.LastInsertId()
	secondProjectID, _ := secondResult.LastInsertId()
	_, err = db.Exec(`INSERT INTO seeds(project_id, type, status, title, priority, created_at) VALUES
		(?, 'idea', 'inbox', '项目一的灵感', 'high', '2026-01-01T00:00:00Z'),
		(?, 'todo', 'doing', '项目二的事项', 'middle', '2026-02-01T00:00:00Z'),
		(?, 'bug', 'done', '项目二的缺陷', 'low', '2026-03-01T00:00:00Z')`, firstProjectID, secondProjectID, secondProjectID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	Register(mux, db)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/seeds?status=inbox,doing", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var items []seed
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ProjectID != secondProjectID || items[1].ProjectID != firstProjectID {
		t.Fatalf("cross-project items = %#v", items)
	}
	if got := recorder.Header().Get("X-Seed-Filtered-Total"); got != "2" {
		t.Fatalf("X-Seed-Filtered-Total = %q, want 2", got)
	}
	if got := recorder.Header().Get("X-Seed-Count-Total"); got != "3" {
		t.Fatalf("X-Seed-Count-Total = %q, want 3", got)
	}
	if got := recorder.Header().Get("X-Seed-Count-Bug"); got != "1" {
		t.Fatalf("X-Seed-Count-Bug = %q, want 1", got)
	}
}

func TestSeedsSearchKeywordInTitleAndContent(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('关键字搜索项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	_, err = db.Exec(`INSERT INTO seeds(project_id, type, status, title, content, priority) VALUES
		(?, 'todo', 'inbox', '实现搜索功能', '仅标题包含关键字', 'middle'),
		(?, 'bug', 'doing', '修复列表问题', '让详细内容支持搜索', 'high'),
		(?, 'feature', 'inbox', '无关种子', '这里没有匹配内容', 'high')`, projectID, projectID, projectID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	Register(mux, db)
	requestSeeds := func(path string) (*httptest.ResponseRecorder, []seed) {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		var items []seed
		if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
			t.Fatal(err)
		}
		return recorder, items
	}

	basePath := "/api/seeds?projectId=" + itoa(projectID) + "&keyword=%E6%90%9C%E7%B4%A2"
	response, items := requestSeeds(basePath)
	if len(items) != 2 || items[0].Title != "修复列表问题" || items[1].Title != "实现搜索功能" {
		t.Fatalf("keyword items = %#v", items)
	}
	if got := response.Header().Get("X-Seed-Filtered-Total"); got != "2" {
		t.Fatalf("X-Seed-Filtered-Total = %q, want 2", got)
	}

	_, items = requestSeeds(basePath + "&priority=high")
	if len(items) != 1 || items[0].Title != "修复列表问题" {
		t.Fatalf("keyword and priority items = %#v", items)
	}
}

func TestWorklogsFilterByCompletionTime(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`INSERT INTO projects(name) VALUES('日志项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	_, err = db.Exec(`INSERT INTO seeds(project_id, type, status, title, priority, completed_at) VALUES
		(?, 'todo', 'done', '范围之前', 'middle', '2026-06-30T23:59:59Z'),
		(?, 'feature', 'done', '七月较早', 'high', '2026-07-01T00:00:00Z'),
		(?, 'bug', 'done', '七月较晚', 'high', '2026-07-31T23:59:59Z'),
		(?, 'todo', 'done', '范围之后', 'low', '2026-08-01T00:00:00Z')`, projectID, projectID, projectID, projectID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	Register(mux, db)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/worklogs?startTime=2026-07-01T00:00:00Z&endTime=2026-08-01T00:00:00Z", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var items []seed
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "七月较晚" || items[1].Title != "七月较早" {
		t.Fatalf("filtered worklogs = %#v", items)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/worklogs?startTime=not-a-time", nil)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid range status = %d, want 400", recorder.Code)
	}
}

func TestSettingsAndProjectManagement(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	activeResult, err := db.Exec(`INSERT INTO projects(name) VALUES('活跃项目')`)
	if err != nil {
		t.Fatal(err)
	}
	activeID, _ := activeResult.LastInsertId()
	archivedResult, err := db.Exec(`INSERT INTO projects(name, archived_at) VALUES('归档项目', strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))`)
	if err != nil {
		t.Fatal(err)
	}
	archivedID, _ := archivedResult.LastInsertId()
	emptyResult, err := db.Exec(`INSERT INTO projects(name) VALUES('空项目')`)
	if err != nil {
		t.Fatal(err)
	}
	emptyID, _ := emptyResult.LastInsertId()
	_, err = db.Exec(`INSERT INTO seeds(project_id, type, status, title, priority, completed_at) VALUES
		(?, 'todo', 'done', '活跃事种', 'middle', '2026-07-22T08:00:00Z'),
		(?, 'todo', 'done', '归档事种', 'middle', '2026-07-22T08:00:00Z')`, activeID, archivedID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	Register(mux, db)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body == nil {
			reader = bytes.NewReader(nil)
		} else {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(encoded)
		}
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(recorder, req)
		return recorder
	}

	response := request(http.MethodGet, "/api/projects", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("active projects status = %d: %s", response.Code, response.Body.String())
	}
	var activeProjects []project
	if err := json.NewDecoder(response.Body).Decode(&activeProjects); err != nil {
		t.Fatal(err)
	}
	if len(activeProjects) != 2 {
		t.Fatalf("active projects = %#v", activeProjects)
	}

	response = request(http.MethodGet, "/api/projects?includeArchived=true", nil)
	var allProjects []project
	if err := json.NewDecoder(response.Body).Decode(&allProjects); err != nil {
		t.Fatal(err)
	}
	if len(allProjects) != 3 || !allProjects[2].Archived || allProjects[2].SeedCount != 1 {
		t.Fatalf("all projects = %#v", allProjects)
	}

	response = request(http.MethodGet, "/api/seeds?status=done", nil)
	var visibleSeeds []seed
	if err := json.NewDecoder(response.Body).Decode(&visibleSeeds); err != nil {
		t.Fatal(err)
	}
	if len(visibleSeeds) != 1 || visibleSeeds[0].Title != "活跃事种" {
		t.Fatalf("visible seeds = %#v", visibleSeeds)
	}
	response = request(http.MethodGet, "/api/worklogs", nil)
	var visibleWorklogs []seed
	if err := json.NewDecoder(response.Body).Decode(&visibleWorklogs); err != nil {
		t.Fatal(err)
	}
	if len(visibleWorklogs) != 1 || visibleWorklogs[0].Title != "活跃事种" {
		t.Fatalf("visible worklogs = %#v", visibleWorklogs)
	}

	response = request(http.MethodDelete, "/api/projects/"+itoa(activeID), nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("non-empty delete status = %d, want 409", response.Code)
	}
	response = request(http.MethodDelete, "/api/projects/"+itoa(emptyID), nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("empty delete status = %d, want 204: %s", response.Code, response.Body.String())
	}

	response = request(http.MethodPatch, "/api/projects/"+itoa(archivedID), map[string]bool{"archived": false})
	if response.Code != http.StatusOK {
		t.Fatalf("restore status = %d: %s", response.Code, response.Body.String())
	}
	var restored project
	if err := json.NewDecoder(response.Body).Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if restored.Archived {
		t.Fatalf("restored project remains archived: %#v", restored)
	}

	response = request(http.MethodGet, "/api/settings", nil)
	var currentSettings settings
	if err := json.NewDecoder(response.Body).Decode(&currentSettings); err != nil {
		t.Fatal(err)
	}
	if currentSettings.WorkdayStart != "10:00" || currentSettings.WorkdayEnd != "19:00" {
		t.Fatalf("default settings = %#v", currentSettings)
	}
	response = request(http.MethodPatch, "/api/settings", settings{WorkdayStart: "18:00", WorkdayEnd: "09:00"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d, want 400", response.Code)
	}
	response = request(http.MethodPatch, "/api/settings", settings{WorkdayStart: "09:30", WorkdayEnd: "18:30"})
	if response.Code != http.StatusOK {
		t.Fatalf("update settings status = %d: %s", response.Code, response.Body.String())
	}
	var settingsUpdatedAt string
	if err := db.QueryRow(`SELECT updated_at FROM app_settings WHERE id=1`).Scan(&settingsUpdatedAt); err != nil {
		t.Fatal(err)
	}
	assertRFC3339UTC(t, settingsUpdatedAt)

	response = request(http.MethodPatch, "/api/projects/"+itoa(activeID), map[string]bool{"archived": true})
	if response.Code != http.StatusOK {
		t.Fatalf("archive status = %d: %s", response.Code, response.Body.String())
	}
	var projectArchivedAt, projectUpdatedAt string
	if err := db.QueryRow(`SELECT archived_at, updated_at FROM projects WHERE id=?`, activeID).
		Scan(&projectArchivedAt, &projectUpdatedAt); err != nil {
		t.Fatal(err)
	}
	assertRFC3339UTC(t, projectArchivedAt)
	assertRFC3339UTC(t, projectUpdatedAt)
}

func TestAppVersion(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	appVersion(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var output map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&output); err != nil {
		t.Fatal(err)
	}
	if output["version"] == "" {
		t.Fatal("version is empty")
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
