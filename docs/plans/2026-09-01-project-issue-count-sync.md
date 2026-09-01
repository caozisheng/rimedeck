# GitLab Project Issue Count Synchronization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refresh open Project list/detail caches immediately after GitLab issue import or reconcile commits issue rows.

**Architecture:** Keep issue counts derived by the existing Project API queries. Add a post-import callback to the transport-free GitLab sync worker; the server wires it to the existing event bus and publishes one `project:updated` event per successful issue snapshot. The existing frontend generic `project:*` realtime refresh already invalidates `projectKeys.all(wsId)`, so no new client protocol behavior is required.

**Tech Stack:** Go, pgx/PostgreSQL, internal events bus, TypeScript, TanStack Query, Vitest.

---

### Task 1: Add failing worker regression tests

**Files:**
- Modify: `server/internal/gitlabsync/worker_test.go`
- Modify: `server/internal/gitlabsync/reconcile_test.go` if the existing reconcile fixture is the closest test seam

**Step 1: Write the failing test**

Add a focused test around `Worker.handleReconcile` (or the smallest existing worker test seam) that injects an `IssueImporter`, supplies a tracker with a valid `ProjectID`, and asserts the post-import callback receives that workspace/project pair exactly once for a non-empty snapshot. Add a second case for an empty snapshot so a remote deletion that changes counts still refreshes Project caches. Keep REST and database behavior injected; do not test React Query here.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./server/internal/gitlabsync -run 'Test.*Project.*Import|Test.*Reconcile.*Project' -count=1
```

Expected: FAIL because the worker has no project-import notification callback or does not invoke it after import.

**Step 3: Commit**

Do not commit separately; keep the red test with the implementation change for one reviewable bug-fix commit.

---

### Task 2: Add post-import project notification contract

**Files:**
- Modify: `server/internal/gitlabsync/worker.go:72-84,247-270`

**Step 1: Define the minimal callback**

Add an optional `OnProjectIssuesImported func(context.Context, db.GitlabTrackerConnection)` field to `Worker`. Invoke it exactly once after `IssueImporter` or `gitlabtracker.ImportIssues` succeeds, including when the issue slice is empty. Do not invoke it on import failure. The worker remains transport-free and the callback is best-effort; the callback cannot change the successful import outcome.

**Step 2: Run the focused test**

Run:

```bash
go test ./server/internal/gitlabsync -run 'Test.*Project.*Import|Test.*Reconcile.*Project' -count=1
```

Expected: PASS.

---

### Task 3: Wire the callback to the existing event bus

**Files:**
- Modify: `server/cmd/server/tracker_import_worker.go:85-100`
- Modify: `server/cmd/server/main.go:164-166,330-330`
- Test: `server/cmd/server/tracker_import_worker_test.go` or an existing server event wiring test

**Step 1: Add event-bus dependency to worker startup**

Pass the existing `*events.Bus` into `runTrackerImportWorker` and wire `OnProjectIssuesImported` to publish `protocol.EventProjectUpdated` with `WorkspaceID` and the tracker `ProjectID`. Use the existing event type and payload convention; the frontend only needs the project prefix to invalidate caches. Convert UUIDs using the repository’s existing utility helpers. Keep event publication after the import returns successfully.

**Step 2: Add/extend a wiring test**

Prove the callback publishes one `project:updated` event with the expected workspace and project IDs. Do not add a second event for each imported issue.

**Step 3: Run targeted server tests**

Run:

```bash
go test ./server/cmd/server ./server/internal/gitlabsync -run 'Test.*(Project|Tracker|Reconcile).*' -count=1
```

Expected: PASS.

---

### Task 4: Verify frontend cache invalidation contract

**Files:**
- Test: `packages/core/issues/ws-updaters.test.ts` or the realtime sync test location matching existing conventions
- Modify: `packages/core/realtime/use-realtime-sync.ts` only if a test proves `project:*` events are not handled by the current generic `refreshMap`

**Step 1: Add a focused cache test only if uncovered**

Assert that a `project:updated` event invalidates `projectKeys.all(wsId)` through the existing generic realtime path. If existing coverage already proves this prefix mapping, retain production code unchanged and record that evidence.

**Step 2: Run the focused frontend test**

Run the repository’s existing package test command for the exact test file, for example:

```bash
pnpm --filter @rimedeck/core test -- ws-updaters.test.ts
```

Expected: PASS with no new client production code unless coverage identifies a real gap.

---

### Task 5: Verify end-to-end behavior and review

**Files:**
- No additional files unless verification exposes a defect.

**Step 1: Run all changed-contract tests**

Run the focused Go and TypeScript tests from Tasks 1–4, then the relevant existing GitLab sync and realtime suites. Do not claim a full repository pass unless the full suite is actually run.

**Step 2: Exercise the behavior**

Run the server/import smoke path available in the repository or use the existing integration test fixture: complete a GitLab import while a Project query is cached, observe a `project:updated` event, and confirm the Project query refetches and reports the imported issue count without restarting the app.

**Step 3: Review the diff**

Request code review. Fix all critical/important findings, then rerun the affected tests.

**Step 4: Commit implementation**

```bash
git add server/cmd/server/main.go server/cmd/server/tracker_import_worker.go server/cmd/server/tracker_import_worker_test.go server/internal/gitlabsync/worker.go server/internal/gitlabsync/worker_test.go server/internal/gitlabsync/reconcile_test.go packages/core/issues/ws-updaters.test.ts packages/core/realtime/use-realtime-sync.ts docs/plans/2026-09-01-project-issue-count-sync-design.md docs/plans/2026-09-01-project-issue-count-sync.md
git commit -m "fix(gitlab): refresh project issue counts after import"
```
