package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- helpers ---

// minimalRuleGoDSL is the smallest valid RuleGo chain DSL that TriggerRun
// can load without error. A single debug-type node is enough to create a
// run row and count exactly 1 total node.
var minimalRuleGoDSL = json.RawMessage(`{
	"ruleChain":{"id":"test","name":"test"},
	"metadata":{
		"nodes":[{"id":"node_1","type":"log","name":"log","configuration":{"jsScript":"return msg;"}}],
		"connections":[]
	}
}`)

// createWorkflowForTest inserts a workflow into the test workspace and
// returns its UUID string. Cleanup runs after the test.
func createWorkflowForTest(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()

	wf, err := testHandler.Queries.CreateWorkflow(ctx, db.CreateWorkflowParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Name:        name,
		Description: "test workflow",
		Icon:        "⚡",
		Category:    "test",
		Graph:       minimalRuleGoDSL,
		Status:      "active",
		CreatedBy:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	wfID := uuidToString(wf.ID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workflow_run WHERE workflow_id = $1`, wfID)
		testPool.Exec(context.Background(), `DELETE FROM agent_workflow WHERE workflow_id = $1`, wfID)
		testPool.Exec(context.Background(), `DELETE FROM workflow WHERE id = $1`, wfID)
	})
	return wfID
}

// bindWorkflowToAgent creates the agent_workflow junction row. Cleaned up
// via the workflow cleanup (CASCADE or explicit DELETE above).
func bindWorkflowToAgent(t *testing.T, agentID, workflowID string) {
	t.Helper()
	err := testHandler.Queries.AddAgentWorkflow(context.Background(), db.AddAgentWorkflowParams{
		AgentID:    parseUUID(agentID),
		WorkflowID: parseUUID(workflowID),
	})
	if err != nil {
		t.Fatalf("bind workflow to agent: %v", err)
	}
}

// ensureWorkflowService wires a real WorkflowService onto testHandler if
// one isn't already set. The service uses testPool for DB access and a
// no-op event bus.
func ensureWorkflowService(t *testing.T) {
	t.Helper()
	if testHandler.WorkflowService != nil {
		return
	}
	ws := service.NewWorkflowService(testHandler.Queries, testPool, testHandler.Bus)
	testHandler.WorkflowService = ws
	t.Cleanup(func() { testHandler.WorkflowService = nil })
}

// countWorkflowRuns returns the number of workflow_run rows for the given
// workflow, optionally filtered by source.
func countWorkflowRuns(t *testing.T, workflowID, source string) int {
	t.Helper()
	var n int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workflow_run WHERE workflow_id = $1 AND source = $2`,
		workflowID, source,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count workflow_run: %v", err)
	}
	return n
}

// --- @mention triggers workflow ---

// TestMentionAgentTriggersWorkflow verifies that when an agent with a
// mounted workflow is @mentioned in a comment, enqueueCommentAgentTriggers
// creates a workflow_run row (source="mention") instead of a coding task.
func TestMentionAgentTriggersWorkflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ensureWorkflowService(t)
	ctx := context.Background()

	// 1. Create agent + workflow + bind
	agentID := createHandlerTestAgent(t, "WFMentionAgent", []byte("[]"))
	wfID := createWorkflowForTest(t, "mention-trigger-wf")
	bindWorkflowToAgent(t, agentID, wfID)

	// 2. Create an issue (needed by enqueueCommentAgentTriggers)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "workflow mention trigger test",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.Unmarshal(w.Body.Bytes(), &issue)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		r := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		r = withURLParam(r, "id", issue.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), r)
	})

	// 3. Post a comment mentioning the agent
	commentContent := fmt.Sprintf("Hey @%s please run the workflow", agentID)
	comment, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID: parseUUID(issue.ID),
		UserID:  parseUUID(testUserID),
		Content: commentContent,
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// 4. Build trigger set (the same path the comment handler takes)
	issueRow, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	triggers := []commentAgentTrigger{{
		Source: commentTriggerSourceMentionAgent,
		Agent:  db.Agent{ID: parseUUID(agentID), Name: "WFMentionAgent"},
	}}
	testHandler.enqueueCommentAgentTriggers(ctx, issueRow, comment.ID, triggers)

	// Give the async TriggerRun a moment to create the run row.
	time.Sleep(200 * time.Millisecond)

	// 5. Assert: a workflow_run with source="mention" was created
	n := countWorkflowRuns(t, wfID, "mention")
	if n == 0 {
		t.Errorf("expected at least 1 workflow_run with source='mention', got 0")
	}

	// 6. Assert: no coding task was enqueued for this agent on this issue
	var taskCount int
	testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND issue_id = $2 AND status IN ('queued','dispatched')`,
		agentID, issue.ID,
	).Scan(&taskCount)
	if taskCount > 0 {
		t.Errorf("expected no coding task for the mentioned agent (workflow took over), got %d", taskCount)
	}
}

// TestMentionAgentFallsBackWithoutWorkflowService verifies that when
// WorkflowService is nil, the @mention path falls back to a coding task.
func TestMentionAgentFallsBackWithoutWorkflowService(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Ensure WorkflowService is nil for this test.
	orig := testHandler.WorkflowService
	testHandler.WorkflowService = nil
	defer func() { testHandler.WorkflowService = orig }()

	agentID := createHandlerTestAgent(t, "WFMentionFallbackAgent", []byte("[]"))
	wfID := createWorkflowForTest(t, "fallback-wf")
	bindWorkflowToAgent(t, agentID, wfID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "workflow mention fallback test",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.Unmarshal(w.Body.Bytes(), &issue)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		r := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		r = withURLParam(r, "id", issue.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), r)
	})

	comment, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID: parseUUID(issue.ID),
		UserID:  parseUUID(testUserID),
		Content: fmt.Sprintf("@%s do something", agentID),
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	issueRow, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	triggers := []commentAgentTrigger{{
		Source: commentTriggerSourceMentionAgent,
		Agent:  db.Agent{ID: parseUUID(agentID), Name: "WFMentionFallbackAgent"},
	}}
	testHandler.enqueueCommentAgentTriggers(ctx, issueRow, comment.ID, triggers)

	// No workflow run should be created.
	n := countWorkflowRuns(t, wfID, "mention")
	if n != 0 {
		t.Errorf("expected 0 workflow_run (service is nil), got %d", n)
	}

	// A coding task should have been enqueued instead.
	var taskCount int
	testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND issue_id = $2`,
		agentID, issue.ID,
	).Scan(&taskCount)
	if taskCount == 0 {
		t.Error("expected a coding task to be enqueued as fallback, got 0")
	}
}

// --- /workflow chat command triggers workflow ---

// TestChatWorkflowCommand verifies that sending "/workflow <name>" in chat
// triggers the matching workflow instead of enqueueing a coding task.
func TestChatWorkflowCommand(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ensureWorkflowService(t)

	agentID := createHandlerTestAgent(t, "WFChatCmdAgent", []byte("[]"))
	wfID := createWorkflowForTest(t, "chat-cmd-wf")
	bindWorkflowToAgent(t, agentID, wfID)
	sessionID := createHandlerTestChatSession(t, agentID)

	// Send "/workflow chat-cmd-wf"
	req := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content": "/workflow chat-cmd-wf",
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.SendChatMessage(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("SendChatMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Give TriggerRun a moment.
	time.Sleep(200 * time.Millisecond)

	// Assert: workflow_run with source="chat" was created.
	n := countWorkflowRuns(t, wfID, "chat")
	if n == 0 {
		t.Error("expected at least 1 workflow_run with source='chat', got 0")
	}

	// Assert: response has no task_id (workflow path skips coding task).
	var resp SendChatMessageResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.TaskID != "" {
		t.Errorf("expected empty task_id when workflow triggered, got %q", resp.TaskID)
	}

	// Assert: an assistant message confirming the trigger was posted.
	var assistantCount int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE chat_session_id = $1 AND role = 'assistant'`,
		sessionID,
	).Scan(&assistantCount)
	if assistantCount == 0 {
		t.Error("expected an assistant confirmation message after /workflow trigger")
	}
}

// TestChatWorkflowCommandNoMatch verifies that "/workflow nonexistent"
// falls through to a normal coding task when no matching workflow is found.
func TestChatWorkflowCommandNoMatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ensureWorkflowService(t)

	agentID := createHandlerTestAgent(t, "WFChatNoMatchAgent", []byte("[]"))
	wfID := createWorkflowForTest(t, "no-match-wf")
	bindWorkflowToAgent(t, agentID, wfID)
	sessionID := createHandlerTestChatSession(t, agentID)

	// Send "/workflow something-else" which doesn't match.
	req := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content": "/workflow something-else",
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.SendChatMessage(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("SendChatMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Assert: NO workflow_run was created.
	n := countWorkflowRuns(t, wfID, "chat")
	if n != 0 {
		t.Errorf("expected 0 workflow_run for non-matching name, got %d", n)
	}

	// Assert: a coding task was enqueued instead.
	var resp SendChatMessageResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.TaskID == "" {
		t.Error("expected a task_id when /workflow doesn't match (fallback to coding task)")
	}
}

// TestChatNormalMessageDoesNotTriggerWorkflow ensures that regular messages
// (without /workflow prefix) go through the normal coding task path even
// when the agent has mounted workflows.
func TestChatNormalMessageDoesNotTriggerWorkflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ensureWorkflowService(t)

	agentID := createHandlerTestAgent(t, "WFChatNormalAgent", []byte("[]"))
	wfID := createWorkflowForTest(t, "normal-msg-wf")
	bindWorkflowToAgent(t, agentID, wfID)
	sessionID := createHandlerTestChatSession(t, agentID)

	req := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content": "Hello, can you help me?",
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.SendChatMessage(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("SendChatMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Assert: no workflow_run created.
	n := countWorkflowRuns(t, wfID, "chat")
	if n != 0 {
		t.Errorf("expected 0 workflow_run for normal message, got %d", n)
	}

	// Assert: coding task was enqueued.
	var resp SendChatMessageResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.TaskID == "" {
		t.Error("expected task_id for normal chat message")
	}
}

// TestChatWorkflowCommandCaseInsensitive verifies that the /workflow name
// match is case-insensitive.
func TestChatWorkflowCommandCaseInsensitive(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ensureWorkflowService(t)

	agentID := createHandlerTestAgent(t, "WFChatCaseAgent", []byte("[]"))
	wfID := createWorkflowForTest(t, "CamelCaseWF")
	bindWorkflowToAgent(t, agentID, wfID)
	sessionID := createHandlerTestChatSession(t, agentID)

	// Send with different casing.
	req := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content": "/workflow camelcasewf",
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.SendChatMessage(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("SendChatMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	time.Sleep(200 * time.Millisecond)

	n := countWorkflowRuns(t, wfID, "chat")
	if n == 0 {
		t.Error("expected workflow trigger to be case-insensitive, got 0 runs")
	}
}
