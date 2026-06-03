package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Shim endpoint tests — guard the BootstrapOnboarding* handlers that were
// restored for desktop < v3 compatibility. Once telemetry confirms no
// pre-v3 desktops remain, delete both these tests AND the handlers in
// onboarding_shim.go in the same commit.
// ---------------------------------------------------------------------------

func TestBootstrapOnboardingRuntimeCreatesSingleGuideIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM agent_task_queue
			 WHERE agent_id IN (
			       SELECT id FROM agent
			        WHERE workspace_id = $1 AND name = $2
			 )
		`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, onboardingIssueTitle,
		)
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, onboardingAssistantName,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, onboardingIssueTitle,
	)
	testPool.Exec(ctx,
		`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, onboardingAssistantName,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
		"runtime_id":   testRuntimeID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkspaceID != testWorkspaceID || resp.AgentID == "" || resp.IssueID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	var (
		agentName    string
		agentRuntime string
		instructions string
		avatarURL    *string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT name, runtime_id, instructions, avatar_url
		  FROM agent
		 WHERE id = $1
	`, resp.AgentID).Scan(&agentName, &agentRuntime, &instructions, &avatarURL); err != nil {
		t.Fatalf("lookup assistant: %v", err)
	}
	if agentName != onboardingAssistantName {
		t.Fatalf("agent name = %q, want %q", agentName, onboardingAssistantName)
	}
	if agentRuntime != testRuntimeID {
		t.Fatalf("agent runtime = %q, want %q", agentRuntime, testRuntimeID)
	}
	if !strings.Contains(instructions, "built-in AI assistant") {
		t.Fatalf("assistant instructions were not seeded with the new identity: %q", instructions)
	}
	if avatarURL == nil || *avatarURL != onboardingAssistantAvatarURL {
		t.Fatalf("agent avatar_url = %v, want seeded Multica Helper avatar", avatarURL)
	}

	var (
		issueTitle    string
		assigneeType  string
		assigneeID    string
		issueStatus   string
		issuePriority string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT title, assignee_type, assignee_id, status, priority
		  FROM issue
		 WHERE id = $1
	`, resp.IssueID).Scan(&issueTitle, &assigneeType, &assigneeID, &issueStatus, &issuePriority); err != nil {
		t.Fatalf("lookup onboarding issue: %v", err)
	}
	if issueTitle != onboardingIssueTitle {
		t.Fatalf("issue title = %q, want %q", issueTitle, onboardingIssueTitle)
	}
	if assigneeType != "agent" || assigneeID != resp.AgentID {
		t.Fatalf("issue assignee = %s/%s, want agent/%s", assigneeType, assigneeID, resp.AgentID)
	}
	if issueStatus != "todo" || issuePriority != "high" {
		t.Fatalf("issue status/priority = %s/%s, want todo/high", issueStatus, issuePriority)
	}

	var (
		onboardedAt         *time.Time
		starterContentState *string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT onboarded_at, starter_content_state
		  FROM "user"
		 WHERE id = $1
	`, testUserID).Scan(&onboardedAt, &starterContentState); err != nil {
		t.Fatalf("lookup user onboarding state: %v", err)
	}
	if onboardedAt == nil {
		t.Fatal("expected onboarded_at to be set")
	}
	if starterContentState == nil || *starterContentState != "imported" {
		t.Fatalf("starter_content_state = %v, want imported", starterContentState)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		  FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2
	`, resp.IssueID, resp.AgentID).Scan(&taskCount); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	if taskCount == 0 {
		t.Fatal("expected onboarding issue to enqueue an agent task")
	}

	w2 := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w2, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w2.Code != http.StatusOK {
		t.Fatalf("second BootstrapOnboardingRuntime: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.AgentID != resp.AgentID || resp2.IssueID != resp.IssueID {
		t.Fatalf("bootstrap should be idempotent: first=%+v second=%+v", resp, resp2)
	}
}

func TestBootstrapOnboardingRuntime_WithStarterPrompt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM agent_task_queue
			 WHERE agent_id IN (
			       SELECT id FROM agent
			        WHERE workspace_id = $1 AND name = $2
			 )
		`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, onboardingIssueTitle,
		)
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, onboardingAssistantName,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, onboardingIssueTitle,
	)
	testPool.Exec(ctx,
		`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, onboardingAssistantName,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
		testUserID,
	)

	const wantPrompt = "Introduce Multica to me, please."
	body := map[string]string{
		"workspace_id":   testWorkspaceID,
		"runtime_id":     testRuntimeID,
		"starter_prompt": wantPrompt,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var description *string
	if err := testPool.QueryRow(ctx, `
		SELECT description FROM issue WHERE id = $1
	`, resp.IssueID).Scan(&description); err != nil {
		t.Fatalf("lookup issue description: %v", err)
	}
	if description == nil || *description != wantPrompt {
		t.Fatalf("issue description = %v, want %q", description, wantPrompt)
	}
}

func TestBootstrapOnboardingRuntime_NoStarterPrompt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM agent_task_queue
			 WHERE agent_id IN (
			       SELECT id FROM agent
			        WHERE workspace_id = $1 AND name = $2
			 )
		`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, onboardingIssueTitle,
		)
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, onboardingAssistantName,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, onboardingIssueTitle,
	)
	testPool.Exec(ctx,
		`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, onboardingAssistantName,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
		"runtime_id":   testRuntimeID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var description *string
	if err := testPool.QueryRow(ctx, `
		SELECT description FROM issue WHERE id = $1
	`, resp.IssueID).Scan(&description); err != nil {
		t.Fatalf("lookup issue description: %v", err)
	}
	if description == nil || *description != onboardingIssueDescription {
		t.Fatalf("issue description = %v, want fallback onboardingIssueDescription", description)
	}
}

func TestBootstrapOnboardingNoRuntimeCreatesSingleGuideIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, noRuntimeIssueTitle,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, noRuntimeIssueTitle,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = 'en' WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingNoRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp bootstrapOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkspaceID != testWorkspaceID || resp.IssueID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	var (
		issueTitle    string
		assigneeType  string
		assigneeID    string
		issueStatus   string
		issuePriority string
		description   string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT title, assignee_type, assignee_id, status, priority, description
		  FROM issue
		 WHERE id = $1
	`, resp.IssueID).Scan(&issueTitle, &assigneeType, &assigneeID, &issueStatus, &issuePriority, &description); err != nil {
		t.Fatalf("lookup no-runtime onboarding issue: %v", err)
	}
	if issueTitle != noRuntimeIssueTitle {
		t.Fatalf("issue title = %q, want %q", issueTitle, noRuntimeIssueTitle)
	}
	if assigneeType != "member" || assigneeID != testUserID {
		t.Fatalf("issue assignee = %s/%s, want member/%s", assigneeType, assigneeID, testUserID)
	}
	if issueStatus != "todo" || issuePriority != "high" {
		t.Fatalf("issue status/priority = %s/%s, want todo/high", issueStatus, issuePriority)
	}
	for _, want := range []string{
		"Try Multica first",
		"https://multica.ai/docs/install-agent-runtime",
		"npm i -g @openai/codex",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("issue description missing %q: %q", want, description)
		}
	}
	if !strings.Contains(description, "Agents need a runtime before they can execute work") {
		t.Fatalf("issue description was not seeded: %q", description)
	}

	var (
		onboardedAt         *time.Time
		starterContentState *string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT onboarded_at, starter_content_state
		  FROM "user"
		 WHERE id = $1
	`, testUserID).Scan(&onboardedAt, &starterContentState); err != nil {
		t.Fatalf("lookup user onboarding state: %v", err)
	}
	if onboardedAt == nil {
		t.Fatal("expected onboarded_at to be set")
	}
	if starterContentState == nil || *starterContentState != "imported" {
		t.Fatalf("starter_content_state = %v, want imported", starterContentState)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		  FROM agent_task_queue
		 WHERE issue_id = $1
	`, resp.IssueID).Scan(&taskCount); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no agent tasks for no-runtime issue, got %d", taskCount)
	}

	w2 := httptest.NewRecorder()
	testHandler.BootstrapOnboardingNoRuntime(w2, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", body))
	if w2.Code != http.StatusOK {
		t.Fatalf("second BootstrapOnboardingNoRuntime: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 bootstrapOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.IssueID != resp.IssueID {
		t.Fatalf("bootstrap should be idempotent: first=%+v second=%+v", resp, resp2)
	}
}

func TestBootstrapOnboardingNoRuntimeUsesChineseGuideForChineseUsers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, noRuntimeIssueTitle,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, noRuntimeIssueTitle,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = 'zh-Hans' WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingNoRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp bootstrapOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var description string
	if err := testPool.QueryRow(ctx, `
		SELECT description
		  FROM issue
		 WHERE id = $1
	`, resp.IssueID).Scan(&description); err != nil {
		t.Fatalf("lookup no-runtime onboarding issue: %v", err)
	}
	for _, want := range []string{
		"先体验项目管理功能",
		"https://multica.ai/docs/install-agent-runtime",
		"中文用户建议先装 Kimi CLI",
		"kimi --version",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("Chinese issue description missing %q: %q", want, description)
		}
	}
}
