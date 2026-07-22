package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"workseed/internal/store"
)

func TestMCPAgentSeedWorkflow(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workseed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	firstProject, err := db.Exec(`INSERT INTO projects(name) VALUES('Agent 项目一')`)
	if err != nil {
		t.Fatal(err)
	}
	secondProject, err := db.Exec(`INSERT INTO projects(name) VALUES('Agent 项目二')`)
	if err != nil {
		t.Fatal(err)
	}
	firstProjectID, _ := firstProject.LastInsertId()
	secondProjectID, _ := secondProject.LastInsertId()
	_, err = db.Exec(`INSERT INTO seeds(project_id, type, status, title, content, priority, created_at) VALUES
		(?, 'todo', 'inbox', '低优先级', '低优先级内容', 'low', '2026-01-01 00:00:00'),
		(?, 'bug', 'inbox', '高优先级', '高优先级内容', 'high', '2026-03-01 00:00:00'),
		(?, 'feature', 'inbox', '中优先级', '中优先级内容', 'middle', '2026-02-01 00:00:00'),
		(?, 'idea', 'done', '已经完成', '', 'high', '2026-04-01 00:00:00')`,
		firstProjectID, secondProjectID, firstProjectID, secondProjectID)
	if err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(Handler(db))
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "workseed-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantTools := []string{"complete_seed", "get_seed", "list_seeds", "skip_seed", "start_seed"}
	if len(tools.Tools) != len(wantTools) {
		t.Fatalf("tools = %#v", tools.Tools)
	}
	found := map[string]bool{}
	for _, tool := range tools.Tools {
		found[tool.Name] = true
	}
	for _, name := range wantTools {
		if !found[name] {
			t.Errorf("tool %q not registered", name)
		}
	}

	listResult := callTool(t, session, "list_seeds", map[string]any{})
	var listed listSeedsOutput
	decodeStructured(t, listResult.StructuredContent, &listed)
	if listed.Count != 3 {
		t.Fatalf("listed count = %d, want 3", listed.Count)
	}
	if listed.Items[0].Title != "高优先级" || listed.Items[1].Title != "中优先级" || listed.Items[2].Title != "低优先级" {
		t.Fatalf("items are not ordered by priority: %#v", listed.Items)
	}
	if listed.Items[0].ProjectName != "Agent 项目二" {
		t.Fatalf("project name = %q, want Agent 项目二", listed.Items[0].ProjectName)
	}

	highPriorityID := listed.Items[0].ID
	highClaimToken := "agent-high-claim"
	getResult := callTool(t, session, "get_seed", map[string]any{"seedId": highPriorityID, "claimToken": highClaimToken})
	var fetched getSeedOutput
	decodeStructured(t, getResult.StructuredContent, &fetched)
	if fetched.Seed.ID != highPriorityID || fetched.Seed.ProjectID != secondProjectID || fetched.Seed.Status != "inbox" || fetched.ClaimedByCaller {
		t.Fatalf("fetched seed = %#v", fetched)
	}

	missingSeed, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_seed", Arguments: map[string]any{"seedId": int64(999999)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !missingSeed.IsError {
		t.Fatal("getting a missing seed unexpectedly succeeded")
	}

	startResult := callTool(t, session, "start_seed", map[string]any{"seedId": highPriorityID, "claimToken": highClaimToken})
	var started transitionOutput
	decodeStructured(t, startResult.StructuredContent, &started)
	if started.Seed.Status != "doing" || started.Seed.StartedAt == nil {
		t.Fatalf("started seed = %#v", started.Seed)
	}
	doingResult := callTool(t, session, "get_seed", map[string]any{"seedId": highPriorityID, "claimToken": highClaimToken})
	var fetchedDoing getSeedOutput
	decodeStructured(t, doingResult.StructuredContent, &fetchedDoing)
	if fetchedDoing.Seed.Status != "doing" || fetchedDoing.Seed.StartedAt == nil || !fetchedDoing.ClaimedByCaller {
		t.Fatalf("fetched doing seed = %#v", fetchedDoing)
	}

	duplicateStart := callTool(t, session, "start_seed", map[string]any{"seedId": highPriorityID, "claimToken": highClaimToken})
	var startedAgain transitionOutput
	decodeStructured(t, duplicateStart.StructuredContent, &startedAgain)
	if startedAgain.Seed.Status != "doing" {
		t.Fatalf("idempotent start seed = %#v", startedAgain.Seed)
	}
	assertToolError(t, session, "start_seed", map[string]any{"seedId": highPriorityID, "claimToken": "other-agent-claim"})
	otherAgentView := callTool(t, session, "get_seed", map[string]any{"seedId": highPriorityID, "claimToken": "other-agent-claim"})
	var fetchedByOther getSeedOutput
	decodeStructured(t, otherAgentView.StructuredContent, &fetchedByOther)
	if fetchedByOther.ClaimedByCaller {
		t.Fatalf("another agent unexpectedly owns seed: %#v", fetchedByOther)
	}

	if _, err := db.Exec(`UPDATE seeds SET started_at = datetime(CURRENT_TIMESTAMP, '-120 seconds') WHERE id = ?`, highPriorityID); err != nil {
		t.Fatal(err)
	}
	assertToolError(t, session, "complete_seed", map[string]any{"seedId": highPriorityID, "claimToken": "other-agent-claim"})
	completeResult := callTool(t, session, "complete_seed", map[string]any{"seedId": highPriorityID, "claimToken": highClaimToken})
	var completed transitionOutput
	decodeStructured(t, completeResult.StructuredContent, &completed)
	if completed.Seed.Status != "done" || completed.Seed.CompletedAt == nil {
		t.Fatalf("completed seed = %#v", completed.Seed)
	}
	if completed.Seed.DurationSeconds == nil || *completed.Seed.DurationSeconds != 120 {
		t.Fatalf("duration = %v, want 120", completed.Seed.DurationSeconds)
	}
	duplicateComplete := callTool(t, session, "complete_seed", map[string]any{"seedId": highPriorityID, "claimToken": highClaimToken})
	var completedAgain transitionOutput
	decodeStructured(t, duplicateComplete.StructuredContent, &completedAgain)
	if completedAgain.Seed.Status != "done" {
		t.Fatalf("idempotent complete seed = %#v", completedAgain.Seed)
	}
	doneResult := callTool(t, session, "get_seed", map[string]any{"seedId": highPriorityID, "claimToken": highClaimToken})
	var fetchedDone getSeedOutput
	decodeStructured(t, doneResult.StructuredContent, &fetchedDone)
	if fetchedDone.Seed.Status != "done" || fetchedDone.Seed.CompletedAt == nil || !fetchedDone.ClaimedByCaller {
		t.Fatalf("fetched done seed = %#v", fetchedDone)
	}

	assertToolError(t, session, "complete_seed", map[string]any{"seedId": listed.Items[1].ID, "claimToken": "unused-claim"})

	skipInboxArgs := map[string]any{"seedId": listed.Items[1].ID, "expectedStatus": "inbox"}
	skipInboxResult := callTool(t, session, "skip_seed", skipInboxArgs)
	var skippedInbox transitionOutput
	decodeStructured(t, skipInboxResult.StructuredContent, &skippedInbox)
	if skippedInbox.Seed.Status != "skipped" || skippedInbox.Seed.CompletedAt != nil {
		t.Fatalf("skipped inbox seed = %#v", skippedInbox.Seed)
	}
	duplicateSkip := callTool(t, session, "skip_seed", skipInboxArgs)
	var skippedAgain transitionOutput
	decodeStructured(t, duplicateSkip.StructuredContent, &skippedAgain)
	if skippedAgain.Seed.Status != "skipped" {
		t.Fatalf("idempotent skip seed = %#v", skippedAgain.Seed)
	}

	lowClaimToken := "agent-low-claim"
	startLow := callTool(t, session, "start_seed", map[string]any{"seedId": listed.Items[2].ID, "claimToken": lowClaimToken})
	var doingLow transitionOutput
	decodeStructured(t, startLow.StructuredContent, &doingLow)
	assertToolError(t, session, "skip_seed", map[string]any{"seedId": listed.Items[2].ID, "expectedStatus": "inbox"})
	assertToolError(t, session, "skip_seed", map[string]any{"seedId": listed.Items[2].ID, "expectedStatus": "doing", "claimToken": "other-agent-claim"})
	skipDoingArgs := map[string]any{"seedId": listed.Items[2].ID, "expectedStatus": "doing", "claimToken": lowClaimToken}
	skipDoingResult := callTool(t, session, "skip_seed", skipDoingArgs)
	var skippedDoing transitionOutput
	decodeStructured(t, skipDoingResult.StructuredContent, &skippedDoing)
	if skippedDoing.Seed.Status != "skipped" || skippedDoing.Seed.StartedAt == nil {
		t.Fatalf("skipped doing seed = %#v", skippedDoing.Seed)
	}
	duplicateDoingSkip := callTool(t, session, "skip_seed", skipDoingArgs)
	var skippedDoingAgain transitionOutput
	decodeStructured(t, duplicateDoingSkip.StructuredContent, &skippedDoingAgain)
	if skippedDoingAgain.Seed.Status != "skipped" {
		t.Fatalf("idempotent doing skip seed = %#v", skippedDoingAgain.Seed)
	}
	claimedSkippedResult := callTool(t, session, "get_seed", map[string]any{"seedId": listed.Items[2].ID, "claimToken": lowClaimToken})
	var claimedSkipped getSeedOutput
	decodeStructured(t, claimedSkippedResult.StructuredContent, &claimedSkipped)
	if claimedSkipped.Seed.Status != "skipped" || !claimedSkipped.ClaimedByCaller {
		t.Fatalf("claimed skipped seed = %#v", claimedSkipped)
	}
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool %s failed: %#v", name, result.Content)
	}
	return result
}

func assertToolError(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("tool %s unexpectedly succeeded: %#v", name, result.StructuredContent)
	}
}

func decodeStructured(t *testing.T, input any, output any) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatal(err)
	}
}
