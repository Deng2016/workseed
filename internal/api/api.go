package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type server struct{ db *sql.DB }

type project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
}

type seed struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Priority  int    `json:"priority"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func Register(mux *http.ServeMux, db *sql.DB) {
	s := &server{db: db}
	mux.HandleFunc("/api/projects", s.projects)
	mux.HandleFunc("/api/seeds", s.seeds)
	mux.HandleFunc("/api/seeds/", s.seedByID)
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
		if status != "" && status != "all" && !contains([]string{"inbox", "planned", "done", "archived"}, status) {
			problem(w, 400, "无效的状态")
			return
		}
		query := `SELECT id, project_id, type, status, title, content, priority, created_at, updated_at FROM seeds WHERE project_id = ?`
		args := []any{projectID}
		if kind != "" && kind != "all" {
			query += ` AND type = ?`
			args = append(args, kind)
		}
		if status != "" && status != "all" {
			query += ` AND status = ?`
			args = append(args, status)
		}
		query += ` ORDER BY CASE status WHEN 'inbox' THEN 0 WHEN 'planned' THEN 1 WHEN 'done' THEN 2 ELSE 3 END, priority DESC, updated_at DESC`
		rows, err := s.db.Query(query, args...)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		defer rows.Close()
		items := []seed{}
		for rows.Next() {
			var item seed
			if err := rows.Scan(&item.ID, &item.ProjectID, &item.Type, &item.Status, &item.Title, &item.Content, &item.Priority, &item.CreatedAt, &item.UpdatedAt); err != nil {
				problem(w, 500, err.Error())
				return
			}
			items = append(items, item)
		}
		writeJSON(w, 200, items)
	case http.MethodPost:
		var in seed
		if err := decode(r, &in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		if err := validateSeed(&in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		if in.Status == "" {
			in.Status = "inbox"
		}
		res, err := s.db.Exec(`INSERT INTO seeds(project_id, type, status, title, content, priority) VALUES(?, ?, ?, ?, ?, ?)`, in.ProjectID, in.Type, in.Status, strings.TrimSpace(in.Title), strings.TrimSpace(in.Content), in.Priority)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		in.ID, _ = res.LastInsertId()
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
		if err := validateSeed(&in); err != nil {
			problem(w, 400, err.Error())
			return
		}
		_, err = s.db.Exec(`UPDATE seeds SET type=?, status=?, title=?, content=?, priority=?, updated_at=CURRENT_TIMESTAMP, completed_at=CASE WHEN ?='done' THEN CURRENT_TIMESTAMP ELSE NULL END WHERE id=?`, in.Type, in.Status, strings.TrimSpace(in.Title), strings.TrimSpace(in.Content), in.Priority, in.Status, id)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		in.ID = id
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
	if s.Status != "" && !contains([]string{"inbox", "planned", "done", "archived"}, s.Status) {
		return errors.New("无效的状态")
	}
	return nil
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
