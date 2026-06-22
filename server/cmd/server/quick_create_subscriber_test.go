package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestQuickCreateCompletion_SubscribesRequester locks in the precreated-issue
// flow: quick-create completion must use the task's existing issue_id rather
// than looking up an issue stamped with origin_type=quick_create.
func TestQuickCreateCompletion_SubscribesRequester(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	// Pre-create the issue (mimics the handler).
	number, err := queries.IncrementIssueCounter(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("IncrementIssueCounter: %v", err)
	}
	issue, err := queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Title:       "server-precreated bug",
		Description: pgtype.Text{String: "placeholder", Valid: true},
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   parseUUID(testUserID),
		Number:      number,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"please file a bug",
		issue.ID,
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	if _, err := taskSvc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	if !isSubscribed(t, queries, util.UUIDToString(issue.ID), "member", testUserID) {
		t.Fatal("expected requester to be subscribed after quick-create completion")
	}
}

// TestQuickCreateFailure_WritesFailureInbox confirms that when a quick-create
// task fails, the completion path writes a failure inbox notification.
func TestQuickCreateFailure_WritesFailureInbox(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	// Pre-create the issue.
	number, err := queries.IncrementIssueCounter(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("IncrementIssueCounter: %v", err)
	}
	issue, err := queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Title:       "will-fail bug",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   parseUUID(testUserID),
		Number:      number,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"another bug",
		issue.ID,
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	// Fail the task — the completion handler should write a failure inbox.
	if _, err := taskSvc.FailTask(ctx, task.ID, "agent crashed", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	// Verify a failure inbox was created.
	var inboxCount int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM inbox_item
		WHERE type = 'quick_create_failed'
		  AND recipient_type = 'member'
		  AND recipient_id = $1
	`, testUserID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	if inboxCount == 0 {
		t.Fatal("expected a quick_create_failed inbox item after task failure")
	}
}
