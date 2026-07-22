package mcpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"workseed/internal/utctime"
	buildversion "workseed/internal/version"
	"workseed/internal/worktime"
)

const (
	defaultListLimit    = 20
	maximumListLimit    = 100
	maxClaimTokenLength = 128
)

type listSeedsInput struct {
	ProjectID int64  `json:"projectId,omitempty" jsonschema:"只获取该项目中的事种；省略时获取所有项目"`
	Status    string `json:"status,omitempty" jsonschema:"事种状态：inbox、doing、paused、skipped、done 或 all；省略时为 inbox"`
	Limit     int    `json:"limit,omitempty" jsonschema:"最多返回的事种数量，默认 20，最大 100"`
}

type getSeedInput struct {
	SeedID     int64  `json:"seedId" jsonschema:"事种 ID"`
	ClaimToken string `json:"claimToken,omitempty" jsonschema:"可选；当前 Agent 为领取操作生成的唯一令牌，用于确认所有权"`
}

type claimSeedInput struct {
	SeedID     int64  `json:"seedId" jsonschema:"事种 ID"`
	ClaimToken string `json:"claimToken" jsonschema:"当前 Agent 为本次领取生成并在后续操作中重复使用的唯一令牌"`
}

type skipSeedInput struct {
	SeedID         int64  `json:"seedId" jsonschema:"事种 ID"`
	ExpectedStatus string `json:"expectedStatus" jsonschema:"期望的当前状态：inbox、doing 或 paused；状态不匹配时拒绝跳过"`
	ClaimToken     string `json:"claimToken,omitempty" jsonschema:"expectedStatus 为 doing 时必填，必须与领取令牌一致"`
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

type getSeedOutput struct {
	Seed            seedOutput `json:"seed"`
	ClaimedByCaller bool       `json:"claimedByCaller"`
}

type transitionOutput struct {
	Message string     `json:"message"`
	Seed    seedOutput `json:"seed"`
}

// Handler returns a Streamable HTTP MCP handler backed by the Workseed database.
func Handler(db *sql.DB) http.Handler {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "workseed", Version: buildversion.Current()},
		&mcp.ServerOptions{Instructions: "先调用 list_seeds 按优先级获取待处理事种。每条事种生成唯一 claimToken，使用同一令牌调用 start_seed、get_seed、complete_seed 及处理失败后的 skip_seed。领取前跳过必须指定 expectedStatus=inbox。工具结果不确定时调用 get_seed 确认状态和 claimedByCaller。"},
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
		Description: "按 ID 获取一条事种的最新信息。传入 claimToken 时同时确认该事种是否由当前调用方领取，但不会回显服务端保存的令牌。",
		Annotations: &mcp.ToolAnnotations{
			Title:         "获取事种详情",
			ReadOnlyHint:  true,
			OpenWorldHint: &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSeedInput) (*mcp.CallToolResult, getSeedOutput, error) {
		output, err := getSeed(ctx, db, input)
		if err != nil {
			return nil, getSeedOutput{}, err
		}
		return nil, output, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_seed",
		Description: "使用唯一 claimToken 原子领取一条 inbox 事种并改为 doing。相同令牌重复调用保持成功，其他令牌不能接管。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "开始处理事种",
			DestructiveHint: &nonDestructive,
			IdempotentHint:  true,
			OpenWorldHint:   &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input claimSeedInput) (*mcp.CallToolResult, transitionOutput, error) {
		item, err := startSeed(ctx, db, input)
		if err != nil {
			return nil, transitionOutput{}, err
		}
		return nil, transitionOutput{Message: "事种已开始处理", Seed: item}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_seed",
		Description: "使用领取时的 claimToken 将 doing 事种改为 done。仅领取者可以完成，相同令牌重复调用保持成功。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "完成事种",
			DestructiveHint: &nonDestructive,
			IdempotentHint:  true,
			OpenWorldHint:   &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input claimSeedInput) (*mcp.CallToolResult, transitionOutput, error) {
		item, err := completeSeed(ctx, db, input)
		if err != nil {
			return nil, transitionOutput{}, err
		}
		return nil, transitionOutput{Message: "事种已完成", Seed: item}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "skip_seed",
		Description: "在当前状态与 expectedStatus 原子匹配时将事种改为 skipped。跳过 doing 事种还必须提供领取时的 claimToken。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "跳过事种",
			DestructiveHint: &nonDestructive,
			IdempotentHint:  true,
			OpenWorldHint:   &closedWorld,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input skipSeedInput) (*mcp.CallToolResult, transitionOutput, error) {
		item, err := skipSeed(ctx, db, input)
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
		FROM seeds s JOIN projects p ON p.id = s.project_id WHERE p.archived_at IS NULL`
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

func getSeed(ctx context.Context, db *sql.DB, input getSeedInput) (getSeedOutput, error) {
	if input.SeedID < 1 {
		return getSeedOutput{}, errors.New("seedId 必须是正整数")
	}
	claimToken, err := normalizeOptionalClaimToken(input.ClaimToken)
	if err != nil {
		return getSeedOutput{}, err
	}
	var item seedOutput
	var storedClaimToken sql.NullString
	err = scanSeedWithClaim(db.QueryRowContext(ctx, `SELECT `+seedColumns+`, s.claim_token
		FROM seeds s JOIN projects p ON p.id = s.project_id WHERE s.id = ? AND p.archived_at IS NULL`, input.SeedID), &item, &storedClaimToken)
	if errors.Is(err, sql.ErrNoRows) {
		return getSeedOutput{}, fmt.Errorf("事种 %d 不存在", input.SeedID)
	}
	if err != nil {
		return getSeedOutput{}, err
	}
	return getSeedOutput{
		Seed:            item,
		ClaimedByCaller: claimToken != "" && claimTokenMatches(storedClaimToken, claimToken),
	}, nil
}

func startSeed(ctx context.Context, db *sql.DB, input claimSeedInput) (seedOutput, error) {
	claimToken, err := validateClaimInput(input)
	if err != nil {
		return seedOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return seedOutput{}, err
	}
	defer tx.Rollback()

	currentStatus, storedClaimToken, err := readSeedClaim(ctx, tx, input.SeedID)
	if err != nil {
		return seedOutput{}, err
	}
	if currentStatus == "doing" && claimTokenMatches(storedClaimToken, claimToken) {
		return commitSeedRead(ctx, tx, input.SeedID)
	}
	if currentStatus != "inbox" {
		return seedOutput{}, fmt.Errorf("事种 %d 当前状态为 %s，不能使用此 claimToken 领取", input.SeedID, currentStatus)
	}
	result, err := tx.ExecContext(ctx, `UPDATE seeds SET status = 'doing', claim_token = ?,
		started_at = CURRENT_TIMESTAMP, completed_at = NULL, duration_seconds = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'inbox'`, claimToken, input.SeedID)
	if err != nil {
		return seedOutput{}, err
	}
	if err := requireSingleRow(result, input.SeedID); err != nil {
		return seedOutput{}, err
	}
	return commitSeedRead(ctx, tx, input.SeedID)
}

func completeSeed(ctx context.Context, db *sql.DB, input claimSeedInput) (seedOutput, error) {
	claimToken, err := validateClaimInput(input)
	if err != nil {
		return seedOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return seedOutput{}, err
	}
	defer tx.Rollback()

	currentStatus, storedClaimToken, err := readSeedClaim(ctx, tx, input.SeedID)
	if err != nil {
		return seedOutput{}, err
	}
	if currentStatus == "done" && claimTokenMatches(storedClaimToken, claimToken) {
		return commitSeedRead(ctx, tx, input.SeedID)
	}
	if currentStatus != "doing" {
		return seedOutput{}, fmt.Errorf("事种 %d 当前状态为 %s，不能标记为已完成", input.SeedID, currentStatus)
	}
	if !claimTokenMatches(storedClaimToken, claimToken) {
		return seedOutput{}, fmt.Errorf("事种 %d 不属于当前 claimToken", input.SeedID)
	}
	result, err := tx.ExecContext(ctx, `UPDATE seeds SET status = 'done', completed_at = CURRENT_TIMESTAMP,
		duration_seconds = NULL,
		updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'doing' AND claim_token = ?`, input.SeedID, claimToken)
	if err != nil {
		return seedOutput{}, err
	}
	if err := requireSingleRow(result, input.SeedID); err != nil {
		return seedOutput{}, err
	}
	var startedAt, completedAt *string
	if err := tx.QueryRowContext(ctx, `SELECT started_at, completed_at FROM seeds WHERE id = ?`, input.SeedID).Scan(&startedAt, &completedAt); err != nil {
		return seedOutput{}, err
	}
	if startedAt != nil && completedAt != nil {
		var workdayStart, workdayEnd string
		if err := tx.QueryRowContext(ctx, `SELECT workday_start, workday_end FROM app_settings WHERE id=1`).Scan(&workdayStart, &workdayEnd); err != nil {
			return seedOutput{}, err
		}
		duration, err := worktime.DurationSecondsForSchedule(*startedAt, *completedAt, workdayStart, workdayEnd)
		if err != nil {
			return seedOutput{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE seeds SET duration_seconds = ? WHERE id = ?`, duration, input.SeedID); err != nil {
			return seedOutput{}, err
		}
	}
	return commitSeedRead(ctx, tx, input.SeedID)
}

func skipSeed(ctx context.Context, db *sql.DB, input skipSeedInput) (seedOutput, error) {
	if input.SeedID < 1 {
		return seedOutput{}, errors.New("seedId 必须是正整数")
	}
	expectedStatus := strings.TrimSpace(input.ExpectedStatus)
	if expectedStatus != "inbox" && expectedStatus != "doing" && expectedStatus != "paused" {
		return seedOutput{}, errors.New("expectedStatus 必须是 inbox、doing 或 paused")
	}
	claimToken, err := normalizeOptionalClaimToken(input.ClaimToken)
	if err != nil {
		return seedOutput{}, err
	}
	if expectedStatus == "doing" && claimToken == "" {
		return seedOutput{}, errors.New("跳过 doing 事种时 claimToken 必填")
	}
	if expectedStatus != "doing" && claimToken != "" {
		return seedOutput{}, errors.New("只有跳过 doing 事种时才可传入 claimToken")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return seedOutput{}, err
	}
	defer tx.Rollback()
	currentStatus, storedClaimToken, err := readSeedClaim(ctx, tx, input.SeedID)
	if err != nil {
		return seedOutput{}, err
	}
	if currentStatus == "skipped" {
		if expectedStatus == "doing" && !claimTokenMatches(storedClaimToken, claimToken) {
			return seedOutput{}, fmt.Errorf("事种 %d 不属于当前 claimToken", input.SeedID)
		}
		return commitSeedRead(ctx, tx, input.SeedID)
	}
	if currentStatus != expectedStatus {
		return seedOutput{}, fmt.Errorf("事种 %d 当前状态为 %s，与 expectedStatus=%s 不匹配", input.SeedID, currentStatus, expectedStatus)
	}
	if expectedStatus == "doing" && !claimTokenMatches(storedClaimToken, claimToken) {
		return seedOutput{}, fmt.Errorf("事种 %d 不属于当前 claimToken", input.SeedID)
	}

	query := `UPDATE seeds SET status = 'skipped', completed_at = NULL, duration_seconds = NULL,
		claim_token = CASE WHEN ? = 'doing' THEN claim_token ELSE NULL END, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ?`
	args := []any{expectedStatus, input.SeedID, expectedStatus}
	if expectedStatus == "doing" {
		query += ` AND claim_token = ?`
		args = append(args, claimToken)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return seedOutput{}, err
	}
	if err := requireSingleRow(result, input.SeedID); err != nil {
		return seedOutput{}, err
	}
	return commitSeedRead(ctx, tx, input.SeedID)
}

func validateClaimInput(input claimSeedInput) (string, error) {
	if input.SeedID < 1 {
		return "", errors.New("seedId 必须是正整数")
	}
	return normalizeClaimToken(input.ClaimToken)
}

func normalizeClaimToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("claimToken 必填")
	}
	if len(value) > maxClaimTokenLength {
		return "", fmt.Errorf("claimToken 长度不能超过 %d", maxClaimTokenLength)
	}
	return value, nil
}

func normalizeOptionalClaimToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	return normalizeClaimToken(value)
}

func claimTokenMatches(stored sql.NullString, claimToken string) bool {
	return stored.Valid && stored.String == claimToken
}

func readSeedClaim(ctx context.Context, tx *sql.Tx, seedID int64) (string, sql.NullString, error) {
	var status string
	var claimToken sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT s.status, s.claim_token FROM seeds s JOIN projects p ON p.id=s.project_id
		WHERE s.id = ? AND p.archived_at IS NULL`, seedID).Scan(&status, &claimToken); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", sql.NullString{}, fmt.Errorf("事种 %d 不存在", seedID)
		}
		return "", sql.NullString{}, err
	}
	return status, claimToken, nil
}

func requireSingleRow(result sql.Result, seedID int64) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("事种 %d 状态已发生变化，请重新获取事种详情", seedID)
	}
	return nil
}

func commitSeedRead(ctx context.Context, tx *sql.Tx, seedID int64) (seedOutput, error) {
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
	if err := row.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.Type, &item.Status,
		&item.Title, &item.Content, &item.Priority, &item.CreatedAt, &item.UpdatedAt,
		&item.StartedAt, &item.CompletedAt, &item.DurationSeconds); err != nil {
		return err
	}
	return normalizeSeedTimes(item)
}

func scanSeedWithClaim(row seedScanner, item *seedOutput, claimToken *sql.NullString) error {
	if err := row.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.Type, &item.Status,
		&item.Title, &item.Content, &item.Priority, &item.CreatedAt, &item.UpdatedAt,
		&item.StartedAt, &item.CompletedAt, &item.DurationSeconds, claimToken); err != nil {
		return err
	}
	return normalizeSeedTimes(item)
}

func normalizeSeedTimes(item *seedOutput) error {
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
	return err
}

func readSeed(ctx context.Context, tx *sql.Tx, seedID int64) (seedOutput, error) {
	var item seedOutput
	err := scanSeed(tx.QueryRowContext(ctx, `SELECT `+seedColumns+`
		FROM seeds s JOIN projects p ON p.id = s.project_id WHERE s.id = ? AND p.archived_at IS NULL`, seedID), &item)
	return item, err
}
