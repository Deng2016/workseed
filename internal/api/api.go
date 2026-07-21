package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	buildversion "workseed/internal/version"
)

type server struct{ db *sql.DB }

type project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
}

type seed struct {
	ID          int64   `json:"id"`
	ProjectID   int64   `json:"projectId"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Title       string  `json:"title"`
	Content     string  `json:"content"`
	Priority    string  `json:"priority"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	StartedAt   *string `json:"startedAt"`
	CompletedAt *string `json:"completedAt"`
	DurationSec *int64  `json:"durationSeconds"`
}

func Register(mux *http.ServeMux, db *sql.DB) {
	s := &server{db: db}
	mux.HandleFunc("/api/projects", s.projects)
	mux.HandleFunc("/api/seeds", s.seeds)
	mux.HandleFunc("/api/seeds/", s.seedByID)
	mux.HandleFunc("/api/worklogs", s.worklogs)
	mux.HandleFunc("/api/version", appVersion)
}

func appVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": buildversion.Current()})
}

func (s *server) projects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := s.db.Query(`SELECT id, name, description, created_at FROM projects ORDER BY updated_at DESC, id DESC`)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		defer rows.Close()
		items := []project{}
		for rows.Next() {
			var p project
			if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt); err != nil {
				problem(w, 500, err.Error())
				return
			}
			items = append(items, p)
		}
		writeJSON(w, 200, items)
	case http.MethodPost:
		var in project
		if err := decode(r, &in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			problem(w, 400, "项目名称不能为空")
			return
		}
		res, err := s.db.Exec(`INSERT INTO projects(name, description) VALUES(?, ?)`, in.Name, strings.TrimSpace(in.Description))
		if err != nil {
			if isProjectNameConflict(err) {
				problem(w, http.StatusConflict, "项目名称已存在")
				return
			}
			problem(w, 500, err.Error())
			return
		}
		in.ID, _ = res.LastInsertId()
		writeJSON(w, 201, in)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) seeds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID, _ := strconv.ParseInt(r.URL.Query().Get("projectId"), 10, 64)
		if projectID == 0 {
			problem(w, 400, "projectId 必填")
			return
		}
		kind := r.URL.Query().Get("type")
		status := r.URL.Query().Get("status")
		if status != "" && status != "all" && !contains([]string{"inbox", "doing", "done"}, status) {
			problem(w, 400, "无效的状态")
			return
		}
		priority := r.URL.Query().Get("priority")
		if priority != "" && priority != "all" && !contains([]string{"high", "middle", "low"}, priority) {
			problem(w, 400, "无效的优先级")
			return
		}
		query := `SELECT ` + seedColumns + ` FROM seeds WHERE project_id = ?`
		args := []any{projectID}
		if kind != "" && kind != "all" {
			query += ` AND type = ?`
			args = append(args, kind)
		}
		if status != "" && status != "all" {
			query += ` AND status = ?`
			args = append(args, status)
		}
		if priority != "" && priority != "all" {
			query += ` AND priority = ?`
			args = append(args, priority)
		}
		query += ` ORDER BY created_at DESC, id DESC`
		rows, err := s.db.Query(query, args...)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		defer rows.Close()
		items := []seed{}
		for rows.Next() {
			var item seed
			if err := scanSeed(rows, &item); err != nil {
				problem(w, 500, err.Error())
				return
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			problem(w, 500, err.Error())
			return
		}
		if err := rows.Close(); err != nil {
			problem(w, 500, err.Error())
			return
		}
		if err := writeSeedCountHeaders(w, s.db, projectID); err != nil {
			problem(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	case http.MethodPost:
		var in seed
		if err := decode(r, &in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		applySeedDefaults(&in)
		if err := validateSeed(&in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		res, err := s.db.Exec(`INSERT INTO seeds(project_id, type, status, title, content, priority, started_at, completed_at)
			VALUES(?, ?, ?, ?, ?, ?, CASE WHEN ?='doing' THEN CURRENT_TIMESTAMP ELSE NULL END, CASE WHEN ?='done' THEN CURRENT_TIMESTAMP ELSE NULL END)`,
			in.ProjectID, in.Type, in.Status, strings.TrimSpace(in.Title), strings.TrimSpace(in.Content), in.Priority, in.Status, in.Status)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		in.ID, _ = res.LastInsertId()
		if err := s.readSeed(in.ID, &in); err != nil {
			problem(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, in)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) seedByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/seeds/"), 10, 64)
	if err != nil {
		problem(w, 404, "种子不存在")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var in seed
		if err := decode(r, &in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		applySeedDefaults(&in)
		if err := validateSeed(&in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		_, err = s.db.Exec(`UPDATE seeds SET
			type=?,
			started_at=CASE WHEN ?='doing' AND status<>'doing' THEN CURRENT_TIMESTAMP ELSE started_at END,
			completed_at=CASE WHEN ?='done' AND status<>'done' THEN CURRENT_TIMESTAMP WHEN ?<>'done' THEN NULL ELSE completed_at END,
			duration_seconds=CASE
				WHEN ?='done' AND status<>'done' AND started_at IS NOT NULL THEN MAX(0, unixepoch(CURRENT_TIMESTAMP)-unixepoch(started_at))
				WHEN ?<>'done' THEN NULL
				ELSE duration_seconds
			END,
			status=?, title=?, content=?, priority=?, updated_at=CURRENT_TIMESTAMP
			WHERE id=?`, in.Type, in.Status, in.Status, in.Status, in.Status, in.Status, in.Status, strings.TrimSpace(in.Title), strings.TrimSpace(in.Content), in.Priority, id)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		if err := s.readSeed(id, &in); err != nil {
			problem(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, in)
	case http.MethodDelete:
		if _, err := s.db.Exec(`DELETE FROM seeds WHERE id=?`, id); err != nil {
			problem(w, 500, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) worklogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	startValue := r.URL.Query().Get("startTime")
	endValue := r.URL.Query().Get("endTime")
	startTime, err := parseOptionalTime(startValue)
	if err != nil {
		problem(w, http.StatusBadRequest, "无效的开始时间")
		return
	}
	endTime, err := parseOptionalTime(endValue)
	if err != nil {
		problem(w, http.StatusBadRequest, "无效的结束时间")
		return
	}
	if startTime != nil && endTime != nil && !startTime.Before(*endTime) {
		problem(w, http.StatusBadRequest, "开始时间必须早于结束时间")
		return
	}

	query := `SELECT ` + seedColumns + ` FROM seeds WHERE completed_at IS NOT NULL`
	args := []any{}
	if startTime != nil {
		query += ` AND unixepoch(completed_at) >= unixepoch(?)`
		args = append(args, startTime.Format(time.RFC3339))
	}
	if endTime != nil {
		query += ` AND unixepoch(completed_at) < unixepoch(?)`
		args = append(args, endTime.Format(time.RFC3339))
	}
	query += ` ORDER BY completed_at DESC, id DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []seed{}
	for rows.Next() {
		var item seed
		if err := scanSeed(rows, &item); err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func writeSeedCountHeaders(w http.ResponseWriter, db *sql.DB, projectID int64) error {
	var total, idea, feature, todo, bug, inbox, doing, done, high, middle, low int
	err := db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(type = 'idea'), 0), COALESCE(SUM(type = 'feature'), 0),
		COALESCE(SUM(type = 'todo'), 0), COALESCE(SUM(type = 'bug'), 0),
		COALESCE(SUM(status = 'inbox'), 0), COALESCE(SUM(status = 'doing'), 0), COALESCE(SUM(status = 'done'), 0),
		COALESCE(SUM(priority = 'high'), 0), COALESCE(SUM(priority = 'middle'), 0),
		COALESCE(SUM(priority = 'low'), 0) FROM seeds WHERE project_id = ?`, projectID).
		Scan(&total, &idea, &feature, &todo, &bug, &inbox, &doing, &done, &high, &middle, &low)
	if err != nil {
		return err
	}
	values := map[string]int{"Total": total, "Idea": idea, "Feature": feature, "Todo": todo, "Bug": bug, "Inbox": inbox, "Doing": doing, "Done": done, "High": high, "Middle": middle, "Low": low}
	for name, value := range values {
		w.Header().Set("X-Seed-Count-"+name, strconv.Itoa(value))
	}
	return nil
}

func applySeedDefaults(s *seed) {
	if s.Status == "" {
		s.Status = "inbox"
	}
	if s.Priority == "" {
		s.Priority = "middle"
	}
}

func validateSeed(s *seed) error {
	if s.ProjectID == 0 {
		return errors.New("projectId 必填")
	}
	if strings.TrimSpace(s.Title) == "" {
		return errors.New("标题不能为空")
	}
	if !contains([]string{"idea", "feature", "todo", "bug"}, s.Type) {
		return errors.New("无效的种子类型")
	}
	if !contains([]string{"inbox", "doing", "done"}, s.Status) {
		return errors.New("无效的状态")
	}
	if !contains([]string{"high", "middle", "low"}, s.Priority) {
		return errors.New("无效的优先级")
	}
	return nil
}

const seedColumns = `id, project_id, type, status, title, content, priority, created_at, updated_at, started_at, completed_at, duration_seconds`

type seedScanner interface {
	Scan(dest ...any) error
}

func scanSeed(row seedScanner, item *seed) error {
	return row.Scan(&item.ID, &item.ProjectID, &item.Type, &item.Status, &item.Title, &item.Content, &item.Priority, &item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt, &item.DurationSec)
}

func (s *server) readSeed(id int64, item *seed) error {
	return scanSeed(s.db.QueryRow(`SELECT `+seedColumns+` FROM seeds WHERE id=?`, id), item)
}

func parseOptionalTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func isProjectNameConflict(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed: projects.name")
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
