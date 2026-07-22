package mcpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	buildversion "workseed/internal/version"
)

const (
	defaultListLimit = 20
	maximumListLimit = 100
)

type listSeedsInput struct {
	ProjectID int64  `json:"projectId,omitempty" jsonschema:"只获取该项目中的事种；省略时获取所有项目"`
	Status    string `json:"status,omitempty" jsonschema:"事种状态：inbox、doing、paused、skipped、done 或 all；省略时为 inbox"`
	Limit     int    `json:"limit,omitempty" jsonschema:"最多返回的事种数量，默认 20，最大 100"`
}

type seedIDInput struct {
	SeedID int64 `json:"seedId" jsonschema:"事种 ID"`
}

type seedOutput struct {
	ID              int64   `json:"id"`
	ProjectID       int64   `json:"projectId"`
	ProjectName     string  `json:"projectName"`
	Type            string  `json:"type"`
	Status          string  `json:"status"`
	Title           string  `json:"title"`
	Content         string  `json:"content"`
	Priority        string  `json:"priority"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	StartedAt       *string `json:"startedAt,omitempty"`
	CompletedAt     *string `json:"completedAt,omitempty"`
	DurationSeconds *int64  `json:"durationSeconds,omitempty"`
}

type listSeedsOutput struct {
	Items []seedOutput `json:"items"`
	Count int          `json:"count"`
}

type transitionOutput struct {
	Message string     `json:"message"`
	Seed    seedOutput `json:"seed"`
}

// Handler returns a Streamable HTTP MCP handler backed by the Workseed database.
func Handler(db *sql.DB) http.Handler {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "workseed", Version: buildversion.Current()},
		&mcp.ServerOptions{Instructions: "先调用 list_seeds 按优先级获取待处理事种。处理前调用 start_seed；工作完成后调用 complete_seed；条件不完整或处理失败时调用 skip_seed。工具结果不确定时调用 get_seed 精确确认状态。不要处理未成功进入 doing 状态的事种。"},
	)

	closedWorld := false
	nonDestructive := false
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_seeds",
		Description: "按高、中、低优先级依次列出事种。默认只返回待实现（inbox）事种，可限定项目、状态和数量。",
		Annotations: &mcp.ToolAnnotations{
			Title:         "按优先级获取事种",
			ReadOnlyHint:  true,
			OpenWorldHint: &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listSeedsInput) (*mcp.CallToolResult, listSeedsOutput, error) {
		items, err := listSeeds(ctx, db, input)
		if err != nil {
			return nil, listSeedsOutput{}, err
		}
		return nil, listSeedsOutput{Items: items, Count: len(items)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_seed",
		Description: "按 ID 获取一条事种的最新信息。用于精确确认项目归属和当前状态。",
		Annotations: &mcp.ToolAnnotations{
			Title:         "获取事种详情",
			ReadOnlyHint:  true,
			OpenWorldHint: &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input seedIDInput) (*mcp.CallToolResult, seedOutput, error) {
		item, err := getSeed(ctx, db, input.SeedID)
		if err != nil {
			return nil, seedOutput{}, err
		}
		return nil, item, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_seed",
		Description: "领取一条待实现事种并将状态原子地改为进行中，同时记录开始时间。只有 inbox 状态可以开始。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "开始处理事种",
			DestructiveHint: &nonDestructive,
			OpenWorldHint:   &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input seedIDInput) (*mcp.CallToolResult, transitionOutput, error) {
		item, err := transitionSeed(ctx, db, input.SeedID, "inbox", "doing")
		if err != nil {
			return nil, transitionOutput{}, err
		}
		return nil, transitionOutput{Message: "事种已开始处理", Seed: item}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_seed",
		Description: "将一条进行中的事种改为已完成，同时记录完成时间并在有开始时间时计算耗时。只有 doing 状态可以完成。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "完成事种",
			DestructiveHint: &nonDestructive,
			OpenWorldHint:   &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input seedIDInput) (*mcp.CallToolResult, transitionOutput, error) {
		item, err := transitionSeed(ctx, db, input.SeedID, "doing", "done")
		if err != nil {
			return nil, transitionOutput{}, err
		}
		return nil, transitionOutput{Message: "事种已完成", Seed: item}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "skip_seed",
		Description: "将一条待实现、进行中或已暂停的事种改为已跳过。用于条件不完整、无法实施或多次尝试仍失败的事种。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "跳过事种",
			DestructiveHint: &nonDestructive,
			IdempotentHint:  true,
			OpenWorldHint:   &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input seedIDInput) (*mcp.CallToolResult, transitionOutput, error) {
		item, err := skipSeed(ctx, db, input.SeedID)
		if err != nil {
			return nil, transitionOutput{}, err
		}
		return nil, transitionOutput{Message: "事种已跳过", Seed: item}, nil
	})

	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}

func listSeeds(ctx context.Context, db *sql.DB, input listSeedsInput) ([]seedOutput, error) {
	if input.ProjectID < 0 {
		return nil, errors.New("projectId 必须是正整数")
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "inbox"
	}
	if status != "all" && status != "inbox" && status != "doing" && status != "paused" && status != "skipped" && status != "done" {
		return nil, errors.New("status 必须是 inbox、doing、paused、skipped、done 或 all")
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultListLimit
	}
	if limit < 1 || limit > maximumListLimit {
		return nil, fmt.Errorf("limit 范围必须是 1-%d", maximumListLimit)
	}

	query := `SELECT ` + seedColumns + `
		FROM seeds s JOIN projects p ON p.id = s.project_id WHERE 1=1`
	args := []any{}
	if input.ProjectID > 0 {
		query += ` AND s.project_id = ?`
		args = append(args, input.ProjectID)
	}
	if status != "all" {
		query += ` AND s.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY CASE s.priority WHEN 'high' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END,
		s.created_at ASC, s.id ASC LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []seedOutput{}
	for rows.Next() {
		var item seedOutput
		if err := scanSeed(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func getSeed(ctx context.Context, db *sql.DB, seedID int64) (seedOutput, error) {
	if seedID < 1 {
		return seedOutput{}, errors.New("seedId 必须是正整数")
	}
	var item seedOutput
	err := scanSeed(db.QueryRowContext(ctx, `SELECT `+seedColumns+`
		FROM seeds s JOIN projects p ON p.id = s.project_id WHERE s.id = ?`, seedID), &item)
	if errors.Is(err, sql.ErrNoRows) {
		return seedOutput{}, fmt.Errorf("事种 %d 不存在", seedID)
	}
	if err != nil {
		return seedOutput{}, err
	}
	return item, nil
}

func transitionSeed(ctx context.Context, db *sql.DB, seedID int64, fromStatus, toStatus string) (seedOutput, error) {
	if seedID < 1 {
		return seedOutput{}, errors.New("seedId 必须是正整数")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return seedOutput{}, err
	}
	defer tx.Rollback()

	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM seeds WHERE id = ?`, seedID).Scan(&currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return seedOutput{}, fmt.Errorf("事种 %d 不存在", seedID)
		}
		return seedOutput{}, err
	}
	if currentStatus != fromStatus {
		return seedOutput{}, fmt.Errorf("事种 %d 当前状态为 %s，只有 %s 状态可以改为 %s", seedID, currentStatus, fromStatus, toStatus)
	}

	var update string
	switch toStatus {
	case "doing":
		update = `UPDATE seeds SET status = 'doing',
			started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
			completed_at = NULL, duration_seconds = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status = 'inbox'`
	case "done":
		update = `UPDATE seeds SET status = 'done', completed_at = CURRENT_TIMESTAMP,
			duration_seconds = CASE WHEN started_at IS NOT NULL
				THEN MAX(0, unixepoch(CURRENT_TIMESTAMP) - unixepoch(started_at)) ELSE NULL END,
			updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'doing'`
	default:
		return seedOutput{}, fmt.Errorf("不支持的目标状态 %s", toStatus)
	}
	result, err := tx.ExecContext(ctx, update, seedID)
	if err != nil {
		return seedOutput{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return seedOutput{}, err
	}
	if affected != 1 {
		return seedOutput{}, fmt.Errorf("事种 %d 状态已发生变化，请重新获取事种列表", seedID)
	}

	item, err := readSeed(ctx, tx, seedID)
	if err != nil {
		return seedOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return seedOutput{}, err
	}
	return item, nil
}

func skipSeed(ctx context.Context, db *sql.DB, seedID int64) (seedOutput, error) {
	if seedID < 1 {
		return seedOutput{}, errors.New("seedId 必须是正整数")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return seedOutput{}, err
	}
	defer tx.Rollback()

	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM seeds WHERE id = ?`, seedID).Scan(&currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return seedOutput{}, fmt.Errorf("事种 %d 不存在", seedID)
		}
		return seedOutput{}, err
	}
	if currentStatus == "done" {
		return seedOutput{}, fmt.Errorf("事种 %d 已完成，不能标记为已跳过", seedID)
	}
	if currentStatus != "skipped" {
		if currentStatus != "inbox" && currentStatus != "doing" && currentStatus != "paused" {
			return seedOutput{}, fmt.Errorf("事种 %d 当前状态为 %s，不能标记为已跳过", seedID, currentStatus)
		}
		result, err := tx.ExecContext(ctx, `UPDATE seeds SET status = 'skipped',
			completed_at = NULL, duration_seconds = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status = ?`, seedID, currentStatus)
		if err != nil {
			return seedOutput{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return seedOutput{}, err
		}
		if affected != 1 {
			return seedOutput{}, fmt.Errorf("事种 %d 状态已发生变化，请重新获取事种列表", seedID)
		}
	}

	item, err := readSeed(ctx, tx, seedID)
	if err != nil {
		return seedOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return seedOutput{}, err
	}
	return item, nil
}

const seedColumns = `s.id, s.project_id, p.name, s.type, s.status, s.title, s.content, s.priority,
	s.created_at, s.updated_at, s.started_at, s.completed_at, s.duration_seconds`

type seedScanner interface {
	Scan(dest ...any) error
}

func scanSeed(row seedScanner, item *seedOutput) error {
	return row.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.Type, &item.Status,
		&item.Title, &item.Content, &item.Priority, &item.CreatedAt, &item.UpdatedAt,
		&item.StartedAt, &item.CompletedAt, &item.DurationSeconds)
}

func readSeed(ctx context.Context, tx *sql.Tx, seedID int64) (seedOutput, error) {
	var item seedOutput
	err := scanSeed(tx.QueryRowContext(ctx, `SELECT `+seedColumns+`
		FROM seeds s JOIN projects p ON p.id = s.project_id WHERE s.id = ?`, seedID), &item)
	return item, err
}
