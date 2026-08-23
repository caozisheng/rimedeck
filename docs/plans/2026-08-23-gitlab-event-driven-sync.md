# GitLab Event-Driven Synchronization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Start GitLab outbox delivery immediately after committed local issue activity while retaining periodic remote reconciliation and durable asynchronous failure handling.

**Architecture:** `main` owns a capacity-one wake channel shared by HTTP handlers and the GitLab sync worker. Committed outbox producers notify it without blocking; the worker distinguishes explicit wakeups from periodic reconciliation and drains ready rows until empty.

**Tech Stack:** Go 1.24, chi, pgx/sqlc, PostgreSQL durable outbox, Go tests.

---

### Task 1: Event-driven worker loop

**Files:**
- Modify: `server/cmd/server/tracker_import_worker.go`
- Test: `server/cmd/server/tracker_import_worker_test.go`

**Step 1: Write failing worker-loop tests**

Add a small `trackerOutboxDrainer` fake. Assert that:

1. sending a wake signal invokes `Tick` without waiting for the periodic interval and does not invoke reconcile scheduling;
2. one wake keeps invoking `Tick` while `Claimed > 0`, then stops after a zero-claim result;
3. a periodic tick schedules reconciliation before draining.

**Step 2: Verify RED**

Run: `cd server && go test ./cmd/server -run 'TestRunTrackerImportLoop' -count=1`

Expected: FAIL because the event-driven loop seam and wake argument do not exist.

**Step 3: Implement the minimal loop**

- Add a narrow `trackerOutboxDrainer` interface exposing `Tick(context.Context) (gitlabsync.TickResult, error)`.
- Extract `runTrackerImportLoop` with injected drainer, reconcile callback, interval, and `<-chan struct{}`.
- Add `drainTrackerOutbox` that repeats until `Claimed == 0` or context cancellation.
- Make wake events drain only; make startup/periodic events schedule then drain.
- Extend `runTrackerImportWorker` to accept the wake receive channel and delegate to the loop.

**Step 4: Verify GREEN**

Run: `cd server && go test ./cmd/server -run 'TestRunTrackerImportLoop' -count=1`

Expected: PASS.

### Task 2: Handler wake contract and wiring

**Files:**
- Modify: `server/internal/handler/handler.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/cmd/server/main.go`
- Test: `server/internal/handler/gitlab_write_ops_test.go`

**Step 1: Write failing handler tests**

Extend GitLab write tests to install a buffered wake channel on the handler. Assert a GitLab issue update emits a signal after successful outbox enqueue. Add a local-issue golden assertion that no signal is emitted.

**Step 2: Verify RED**

Run: `cd server && go test ./internal/handler -run 'Test(UpdateIssueGitlab_WakesSyncWorker|UpdateIssueLocal_SkipsSyncWake)' -count=1`

Expected: FAIL because `Handler` has no GitLab sync wake channel.

**Step 3: Implement the wake contract**

- Add a send-only wake channel to `handler.Handler` and a private non-blocking notification helper.
- Add the channel to `RouterOptions` and inject it into the constructed handler.
- In `main`, create one capacity-one channel before constructing the router; pass it to both router options and `runTrackerImportWorker`.

**Step 4: Verify GREEN**

Run the Task 2 test command again. Expected: PASS.

### Task 3: Notify every durable explicit-work producer

**Files:**
- Modify: `server/internal/handler/gitlab_tracker.go`
- Modify: `server/internal/handler/issue.go`
- Test: `server/internal/handler/gitlab_write_ops_test.go`
- Test: `server/internal/handler/gitlab_webhook_ingress_test.go`
- Test: `server/internal/handler/gitlab_tracker_lifecycle_test.go`

**Step 1: Write failing producer tests**

Cover these observable boundaries:

- GitLab issue create wakes only after its issue/outbox transaction commits.
- `enqueueGitlabWriteOp` wakes after its revision/outbox transaction commits, covering update/delete/labels and batch variants.
- webhook enqueue wakes after the outbox insert succeeds.
- manual sync and failed-outbox retry wake after their database operation succeeds.

Use a capacity-one test channel and consume between actions. Do not assert implementation details beyond signal presence/absence.

**Step 2: Verify RED**

Run: `cd server && go test ./internal/handler -run 'Gitlab.*Wake|Webhook.*Wake|Retry.*Wake|ManualSync.*Wake' -count=1`

Expected: FAIL for producer paths not yet notifying.

**Step 3: Implement notifications**

Call the helper only after successful commit/enqueue/reset. Do not notify on validation errors, database errors, ignored/duplicate webhooks, or local-only issue operations.

**Step 4: Verify GREEN**

Run the Task 3 command again. Expected: PASS.

### Task 4: Regression and behavioral verification

**Files:**
- Modify only if a verified regression requires it.

**Step 1: Run focused synchronization tests**

Run: `cd server && go test ./cmd/server ./internal/gitlabsync ./internal/handler -run 'Tracker|Gitlab|GitLab|Outbox|Reconcile|Webhook' -count=1`

Expected: PASS.

**Step 2: Run full affected-package tests**

Run: `cd server && go test ./cmd/server ./internal/gitlabsync ./internal/handler -count=1`

Expected: PASS.

**Step 3: Run static verification**

Run: `cd server && go vet ./cmd/server ./internal/gitlabsync ./internal/handler`

Expected: exit 0.

**Step 4: Review the final change**

Confirm:

- no GitLab REST request moved into an HTTP handler;
- every wake follows durable database success;
- local issues emit no wake;
- periodic reconcile remains active;
- channel sends cannot block request handlers.
