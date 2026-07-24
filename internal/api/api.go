package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"workseed/internal/mcpserver"
	"workseed/internal/utctime"
	buildversion "workseed/internal/version"
	"workseed/internal/worktime"
)

type server struct{ db *sql.DB }

type project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	Archived    bool   `json:"archived"`
	SeedCount   int64  `json:"seedCount"`
}

type settings struct {
	WorkdayStart string `json:"workdayStart"`
	WorkdayEnd   string `json:"workdayEnd"`
}

type seed struct {
	ID          int64        `json:"id"`
	ProjectID   int64        `json:"projectId"`
	Type        string       `json:"type"`
	Status      string       `json:"status"`
	Title       string       `json:"title"`
	Content     string       `json:"content"`
	Priority    string       `json:"priority"`
	CreatedAt   string       `json:"createdAt"`
	UpdatedAt   string       `json:"updatedAt"`
	StartedAt   *string      `json:"startedAt"`
	CompletedAt *string      `json:"completedAt"`
	DurationSec *int64       `json:"durationSeconds"`
	Workpad     *seedWorkpad `json:"workpad,omitempty"`
}

type seedWorkpad struct {
	InputTokens            int64   `json:"inputTokens"`
	OutputTokens           int64   `json:"outputTokens"`
	TotalTokens            int64   `json:"totalTokens"`
	CommitTime             *string `json:"commitTime,omitempty"`
	CommitID               string  `json:"commitId"`
	ImplementationApproach string  `json:"implementationApproach"`
	Changes                string  `json:"changes"`
}

func Register(mux *http.ServeMux, db *sql.DB) {
	s := &server{db: db}
	mcpHandler := mcpserver.Handler(db)
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)
	mux.HandleFunc("/api/projects", s.projects)
	mux.HandleFunc("/api/projects/", s.projectByID)
	mux.HandleFunc("/api/settings", s.settings)
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
		query := `SELECT p.id, p.name, p.description, p.created_at, p.archived_at IS NOT NULL,
			(SELECT COUNT(*) FROM seeds s WHERE s.project_id = p.id)
			FROM projects p`
		if r.URL.Query().Get("includeArchived") != "true" {
			query += ` WHERE p.archived_at IS NULL`
		}
		query += ` ORDER BY p.archived_at IS NOT NULL, p.updated_at DESC, p.id DESC`
		rows, err := s.db.Query(query)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		defer rows.Close()
		items := []project{}
		for rows.Next() {
			var p project
			if err := scanProject(rows, &p); err != nil {
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
		if err := s.readProject(in.ID, &in); err != nil {
			problem(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, in)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) projectByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/projects/"), 10, 64)
	if err != nil || id < 1 {
		problem(w, http.StatusNotFound, "项目不存在")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var in struct {
			Archived bool `json:"archived"`
		}
		if err := decode(r, &in); err != nil {
			problem(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := s.db.Exec(`UPDATE projects SET archived_at=CASE WHEN ? THEN COALESCE(archived_at, strftime('%Y-%m-%dT%H:%M:%SZ', 'now')) ELSE NULL END,
			updated_at=strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id=?`, in.Archived, id)
		if err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		if affected != 1 {
			problem(w, http.StatusNotFound, "项目不存在")
			return
		}
		var output project
		if err := s.readProject(id, &output); err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, output)
	case http.MethodDelete:
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer tx.Rollback()
		var seedCount int64
		if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(s.id) FROM projects p LEFT JOIN seeds s ON s.project_id=p.id WHERE p.id=?`, id).Scan(&seedCount); err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		if seedCount > 0 {
			problem(w, http.StatusConflict, "只能删除没有事种的空项目")
			return
		}
		result, err := tx.ExecContext(r.Context(), `DELETE FROM projects WHERE id=?`, id)
		if err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		if affected != 1 {
			problem(w, http.StatusNotFound, "项目不存在")
			return
		}
		if err := tx.Commit(); err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var output settings
		if err := readSettings(r.Context(), s.db, &output); err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, output)
	case http.MethodPatch:
		var in settings
		if err := decode(r, &in); err != nil {
			problem(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateSettings(in); err != nil {
			problem(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := s.db.ExecContext(r.Context(), `UPDATE app_settings SET workday_start=?, workday_end=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id=1`, in.WorkdayStart, in.WorkdayEnd); err != nil {
			problem(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, in)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) seeds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		queryValues := r.URL.Query()
		var projectID int64
		if projectValue := strings.TrimSpace(queryValues.Get("projectId")); projectValue != "" {
			var err error
			projectID, err = strconv.ParseInt(projectValue, 10, 64)
			if err != nil || projectID < 1 {
				problem(w, 400, "无效的 projectId")
				return
			}
		}
		page, err := parsePositiveQueryInt(queryValues.Get("page"), 1)
		if err != nil {
			problem(w, 400, "无效的页码")
			return
		}
		pageSize, err := parsePositiveQueryInt(queryValues.Get("pageSize"), 20)
		if err != nil || pageSize > 100 {
			problem(w, 400, "无效的每页数量（范围为 1-100）")
			return
		}
		if page-1 > math.MaxInt64/pageSize {
			problem(w, 400, "页码过大")
			return
		}
		kinds, kindsSupplied, err := parseMultiFilter(queryValues, "type", []string{"idea", "feature", "todo", "bug"})
		if err != nil {
			problem(w, 400, "无效的种子类型")
			return
		}
		statuses, statusesSupplied, err := parseMultiFilter(queryValues, "status", []string{"inbox", "doing", "paused", "skipped", "done"})
		if err != nil {
			problem(w, 400, "无效的状态")
			return
		}
		priorities, prioritiesSupplied, err := parseMultiFilter(queryValues, "priority", []string{"high", "middle", "low"})
		if err != nil {
			problem(w, 400, "无效的优先级")
			return
		}
		where := ` WHERE EXISTS (SELECT 1 FROM projects p WHERE p.id=seeds.project_id AND p.archived_at IS NULL)`
		args := []any{}
		if projectID > 0 {
			where += ` AND project_id = ?`
			args = append(args, projectID)
		}
		where, args = appendMultiFilter(where, args, "type", kinds, kindsSupplied)
		where, args = appendMultiFilter(where, args, "status", statuses, statusesSupplied)
		where, args = appendMultiFilter(where, args, "priority", priorities, prioritiesSupplied)
		keyword := strings.TrimSpace(queryValues.Get("keyword"))
		if keyword != "" {
			where += ` AND (title LIKE ? OR content LIKE ?)`
			pattern := "%" + keyword + "%"
			args = append(args, pattern, pattern)
		}
		var filteredTotal int64
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM seeds`+where, args...).Scan(&filteredTotal); err != nil {
			problem(w, 500, err.Error())
			return
		}
		offset := (page - 1) * pageSize
		query := `SELECT ` + seedColumns + ` FROM seeds` + where + ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
		args = append(args, pageSize, offset)
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
		hasMore := offset < filteredTotal && int64(len(items)) < filteredTotal-offset
		w.Header().Set("X-Seed-Page", strconv.FormatInt(page, 10))
		w.Header().Set("X-Seed-Page-Size", strconv.FormatInt(pageSize, 10))
		w.Header().Set("X-Seed-Filtered-Total", strconv.FormatInt(filteredTotal, 10))
		w.Header().Set("X-Seed-Has-More", strconv.FormatBool(hasMore))
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
		if err := s.requireActiveProject(r.Context(), in.ProjectID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				problem(w, http.StatusBadRequest, "项目不存在或已归档")
			} else {
				problem(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		res, err := s.db.Exec(`INSERT INTO seeds(project_id, type, status, title, content, priority, started_at, completed_at)
			VALUES(?, ?, ?, ?, ?, ?, CASE WHEN ?='doing' THEN strftime('%Y-%m-%dT%H:%M:%SZ', 'now') ELSE NULL END, CASE WHEN ?='done' THEN strftime('%Y-%m-%dT%H:%M:%SZ', 'now') ELSE NULL END)`,
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
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		var previousStatus string
		if err := tx.QueryRowContext(r.Context(), `SELECT s.status FROM seeds s JOIN projects p ON p.id=s.project_id WHERE s.id=? AND p.archived_at IS NULL`, id).Scan(&previousStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				problem(w, http.StatusNotFound, "种子不存在或所属项目已归档")
				return
			}
			problem(w, 500, err.Error())
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE seeds SET
			type=?,
			started_at=CASE WHEN ?='doing' AND status<>'doing' THEN strftime('%Y-%m-%dT%H:%M:%SZ', 'now') ELSE started_at END,
			completed_at=CASE WHEN ?='done' AND status<>'done' THEN strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHEN ?<>'done' THEN NULL ELSE completed_at END,
			duration_seconds=CASE
				WHEN ?='done' AND status<>'done' THEN NULL
				WHEN ?<>'done' THEN NULL
				ELSE duration_seconds
			END,
			claim_token=CASE
				WHEN ?=status THEN claim_token
				WHEN status='doing' AND ? IN ('done', 'skipped') THEN claim_token
				ELSE NULL
			END,
			status=?, title=?, content=?, priority=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id=?`,
			in.Type,
			in.Status,
			in.Status, in.Status,
			in.Status, in.Status,
			in.Status, in.Status,
			in.Status, strings.TrimSpace(in.Title), strings.TrimSpace(in.Content), in.Priority, id)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		if in.Status == "done" && previousStatus != "done" {
			var startedAt, completedAt *string
			if err := tx.QueryRowContext(r.Context(), `SELECT started_at, completed_at FROM seeds WHERE id=?`, id).Scan(&startedAt, &completedAt); err != nil {
				problem(w, 500, err.Error())
				return
			}
			if startedAt != nil && completedAt != nil {
				var appSettings settings
				if err := readSettings(r.Context(), tx, &appSettings); err != nil {
					problem(w, 500, err.Error())
					return
				}
				duration, err := worktime.DurationSecondsForSchedule(*startedAt, *completedAt, appSettings.WorkdayStart, appSettings.WorkdayEnd)
				if err != nil {
					problem(w, 500, err.Error())
					return
				}
				if _, err := tx.ExecContext(r.Context(), `UPDATE seeds SET duration_seconds=? WHERE id=?`, duration, id); err != nil {
					problem(w, 500, err.Error())
					return
				}
			}
		}
		if err := scanSeed(tx.QueryRowContext(r.Context(), `SELECT `+seedColumns+` FROM seeds WHERE id=?`, id), &in); err != nil {
			problem(w, 500, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			problem(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, in)
	case http.MethodDelete:
		if _, err := s.db.Exec(`DELETE FROM seeds WHERE id=? AND EXISTS (SELECT 1 FROM projects p WHERE p.id=seeds.project_id AND p.archived_at IS NULL)`, id); err != nil {
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

	query := `SELECT ` + seedColumns + ` FROM seeds WHERE completed_at IS NOT NULL
		AND EXISTS (SELECT 1 FROM projects p WHERE p.id=seeds.project_id AND p.archived_at IS NULL)`
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
	var total, idea, feature, todo, bug, inbox, doing, paused, skipped, done, high, middle, low int
	query := `SELECT COUNT(*),
		COALESCE(SUM(type = 'idea'), 0), COALESCE(SUM(type = 'feature'), 0),
		COALESCE(SUM(type = 'todo'), 0), COALESCE(SUM(type = 'bug'), 0),
		COALESCE(SUM(status = 'inbox'), 0), COALESCE(SUM(status = 'doing'), 0),
		COALESCE(SUM(status = 'paused'), 0), COALESCE(SUM(status = 'skipped'), 0), COALESCE(SUM(status = 'done'), 0),
		COALESCE(SUM(priority = 'high'), 0), COALESCE(SUM(priority = 'middle'), 0),
		COALESCE(SUM(priority = 'low'), 0) FROM seeds
		WHERE EXISTS (SELECT 1 FROM projects p WHERE p.id=seeds.project_id AND p.archived_at IS NULL)`
	args := []any{}
	if projectID > 0 {
		query += ` AND project_id = ?`
		args = append(args, projectID)
	}
	err := db.QueryRow(query, args...).
		Scan(&total, &idea, &feature, &todo, &bug, &inbox, &doing, &paused, &skipped, &done, &high, &middle, &low)
	if err != nil {
		return err
	}
	values := map[string]int{"Total": total, "Idea": idea, "Feature": feature, "Todo": todo, "Bug": bug, "Inbox": inbox, "Doing": doing, "Paused": paused, "Skipped": skipped, "Done": done, "High": high, "Middle": middle, "Low": low}
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
	if !contains([]string{"inbox", "doing", "paused", "skipped", "done"}, s.Status) {
		return errors.New("无效的状态")
	}
	if !contains([]string{"high", "middle", "low"}, s.Priority) {
		return errors.New("无效的优先级")
	}
	return nil
}

const seedColumns = `id, project_id, type, status, title, content, priority, created_at, updated_at, started_at, completed_at, duration_seconds,
	(SELECT input_tokens FROM seed_workpads WHERE seed_id=seeds.id),
	(SELECT output_tokens FROM seed_workpads WHERE seed_id=seeds.id),
	(SELECT total_tokens FROM seed_workpads WHERE seed_id=seeds.id),
	(SELECT commit_at FROM seed_workpads WHERE seed_id=seeds.id),
	(SELECT commit_id FROM seed_workpads WHERE seed_id=seeds.id),
	(SELECT implementation FROM seed_workpads WHERE seed_id=seeds.id),
	(SELECT changes FROM seed_workpads WHERE seed_id=seeds.id)`

type seedScanner interface {
	Scan(dest ...any) error
}

func scanSeed(row seedScanner, item *seed) error {
	var inputTokens, outputTokens, totalTokens sql.NullInt64
	var commitTime, commitID, implementation, changes sql.NullString
	if err := row.Scan(&item.ID, &item.ProjectID, &item.Type, &item.Status, &item.Title, &item.Content,
		&item.Priority, &item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		&item.DurationSec, &inputTokens, &outputTokens, &totalTokens, &commitTime, &commitID,
		&implementation, &changes); err != nil {
		return err
	}
	if inputTokens.Valid {
		item.Workpad = &seedWorkpad{
			InputTokens:            inputTokens.Int64,
			OutputTokens:           outputTokens.Int64,
			TotalTokens:            totalTokens.Int64,
			CommitID:               commitID.String,
			ImplementationApproach: implementation.String,
			Changes:                changes.String,
		}
		if commitTime.Valid {
			item.Workpad.CommitTime = &commitTime.String
		}
	}
	return normalizeSeedTimes(item)
}

func (s *server) readSeed(id int64, item *seed) error {
	return scanSeed(s.db.QueryRow(`SELECT `+seedColumns+` FROM seeds WHERE id=?
		AND EXISTS (SELECT 1 FROM projects p WHERE p.id=seeds.project_id AND p.archived_at IS NULL)`, id), item)
}

func scanProject(row seedScanner, item *project) error {
	if err := row.Scan(&item.ID, &item.Name, &item.Description, &item.CreatedAt, &item.Archived, &item.SeedCount); err != nil {
		return err
	}
	formatted, err := utctime.FormatRFC3339(item.CreatedAt)
	if err != nil {
		return err
	}
	item.CreatedAt = formatted
	return nil
}

func normalizeSeedTimes(item *seed) error {
	for _, value := range []*string{&item.CreatedAt, &item.UpdatedAt} {
		formatted, err := utctime.FormatRFC3339(*value)
		if err != nil {
			return err
		}
		*value = formatted
	}
	var err error
	item.StartedAt, err = utctime.FormatOptionalRFC3339(item.StartedAt)
	if err != nil {
		return err
	}
	item.CompletedAt, err = utctime.FormatOptionalRFC3339(item.CompletedAt)
	if err != nil {
		return err
	}
	if item.Workpad != nil {
		item.Workpad.CommitTime, err = utctime.FormatOptionalRFC3339(item.Workpad.CommitTime)
	}
	return err
}

func (s *server) readProject(id int64, item *project) error {
	return scanProject(s.db.QueryRow(`SELECT p.id, p.name, p.description, p.created_at, p.archived_at IS NOT NULL,
		(SELECT COUNT(*) FROM seeds s WHERE s.project_id=p.id) FROM projects p WHERE p.id=?`, id), item)
}

type contextRowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readSettings(ctx context.Context, queryer contextRowQueryer, output *settings) error {
	return queryer.QueryRowContext(ctx, `SELECT workday_start, workday_end FROM app_settings WHERE id=1`).Scan(&output.WorkdayStart, &output.WorkdayEnd)
}

func validateSettings(value settings) error {
	start, err := time.Parse("15:04", value.WorkdayStart)
	if err != nil || start.Format("15:04") != value.WorkdayStart {
		return errors.New("上班时间必须使用 HH:MM 格式")
	}
	end, err := time.Parse("15:04", value.WorkdayEnd)
	if err != nil || end.Format("15:04") != value.WorkdayEnd {
		return errors.New("下班时间必须使用 HH:MM 格式")
	}
	if !start.Before(end) {
		return errors.New("下班时间必须晚于上班时间")
	}
	return nil
}

func (s *server) requireActiveProject(ctx context.Context, projectID int64) error {
	var exists int
	return s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id=? AND archived_at IS NULL`, projectID).Scan(&exists)
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

func parsePositiveQueryInt(value string, defaultValue int64) (int64, error) {
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, errors.New("value must be a positive integer")
	}
	return parsed, nil
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

func parseMultiFilter(query url.Values, key string, allowed []string) ([]string, bool, error) {
	rawValues, supplied := query[key]
	if !supplied {
		return nil, false, nil
	}
	selected := []string{}
	seen := map[string]bool{}
	for _, raw := range rawValues {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if value == "all" {
				return nil, false, nil
			}
			if !contains(allowed, value) {
				return nil, true, errors.New("invalid filter value")
			}
			if !seen[value] {
				selected = append(selected, value)
				seen[value] = true
			}
		}
	}
	return selected, true, nil
}

func appendMultiFilter(query string, args []any, column string, selected []string, supplied bool) (string, []any) {
	if !supplied {
		return query, args
	}
	if len(selected) == 0 {
		return query + ` AND 1=0`, args
	}
	query += ` AND ` + column + ` IN (` + strings.TrimSuffix(strings.Repeat("?,", len(selected)), ",") + `)`
	for _, value := range selected {
		args = append(args, value)
	}
	return query, args
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
