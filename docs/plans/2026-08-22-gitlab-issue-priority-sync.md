# GitLab Issue Priority Sync Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make GitLab-mirrored Issue priority changes synchronize bidirectionally through canonical `priority::urgent/high/medium/low` labels while preserving ordinary and workflow labels.

**Architecture:** Reuse the existing GitLab label mapping, local-first Issue update, `update_issue` outbox, destructive full-label PUT, and revision-guarded canonical apply paths. Add `priority::urgent` to the centralized mapping and close the loop with focused importer, handler, and worker regression tests. `none` remains the no-priority state and removes all priority mapping labels.

**Tech Stack:** Go, PostgreSQL/sqlc, pgx, GitLab REST API v4, httptest, existing integration-test harness.

---

### Task 1: Add failing urgent mapping tests

**Files:**
- Modify: `server/internal/gitlabtracker/mapping_test.go`

**Step 1: Write the failing tests**

Extend the table-driven mapping tests to assert:

- `ClassifyLabel("priority::urgent") == MappingPriority`;
- `ProjectIssueFields("opened", []string{"priority::urgent"})` returns `backlog, urgent`;
- multiple priority labels select `urgent > high > medium > low`;
- `CanonicalLabels("todo", "urgent", []string{"bug", "priority::low"})` returns `bug`, `workflow::todo`, `priority::urgent`;
- `CanonicalLabels("todo", "none", []string{"priority::none", "priority::high"})` retains `priority::none` as an unknown ordinary label but removes `priority::high`.

**Step 2: Run the focused test and verify RED**

Run:

```bash
cd server && go test ./internal/gitlabtracker -run 'Test(Classify|Project|Canonical)' -count=1
```

Expected: FAIL because `priority::urgent` is not classified or reverse-mapped and the precedence is absent.

**Step 3: Commit checkpoint**

Do not commit production changes before the failing test is observed. Continue to Task 2 after confirming the failure is due to the missing mapping.

### Task 2: Implement centralized urgent priority mapping

**Files:**
- Modify: `server/internal/gitlabtracker/mapping.go`

**Step 1: Add the minimal mapping**

Add `priority::urgent` to `priorityByLabel` and `urgent: priority::urgent` to `labelByPriority`. Update `priorityRank` so urgent ranks above high. Keep `none` absent from both maps. Do not parse arbitrary label prefixes.

**Step 2: Run mapping tests and verify GREEN**

Run:

```bash
cd server && go test ./internal/gitlabtracker -run 'Test(Classify|Project|Canonical)' -count=1
```

Expected: PASS.

**Step 3: Commit**

```bash
git add server/internal/gitlabtracker/mapping.go server/internal/gitlabtracker/mapping_test.go
git commit -m "feat(gitlab): map urgent issue priority"
```

### Task 3: Add failing importer/canonical urgent projection coverage

**Files:**
- Modify: `server/internal/handler/gitlab_canonical_test.go`

**Step 1: Write the failing integration assertions**

Add a database-backed test using the existing tracker/project helpers that:

- imports a GitLab label snapshot containing `priority::urgent` and an ordinary label;
- imports an Issue carrying both labels;
- asserts local `issue.priority = 'urgent'`;
- asserts the `priority::urgent` definition has `mapping_kind = 'priority'`;
- asserts both label relations remain stored while only the ordinary label is visible through the visible query;
- applies a canonical remote response with `priority::urgent` and asserts the same projection on an existing linked Issue.

Use the repository's existing `testHandler`/`testPool` skip convention when the integration database is unavailable.

**Step 2: Run the focused test and verify RED**

Run:

```bash
cd server && go test ./internal/handler -run 'Test.*(Urgent|ImportIssues|ApplyCanonical)' -count=1
```

Expected: FAIL at the priority projection or mapping-kind assertion before implementation is complete.

**Step 3: Confirm import code path**

Verify the failure reaches `ProjectIssueFields`/label import rather than failing from fixture setup. Do not change importer behavior unless the focused failure demonstrates a missing call path; both import and canonical apply already use the centralized projection.

### Task 4: Add handler regression coverage for priority-only local updates

**Files:**
- Modify: `server/internal/handler/gitlab_write_ops_test.go`

**Step 1: Write failing/guarding tests**

Add a test for a linked GitLab Issue seeded with:

- local `status = 'in_review'`, `priority = 'high'`;
- same-tracker ordinary label `bug`;
- mapped workflow and priority relations, including stale `priority::high`.

Update only priority to `urgent` and assert:

- the local row changes first;
- one pending `update_issue` outbox row is created;
- payload labels are exactly ordinary `bug`, canonical `workflow::in-review`, and `priority::urgent`;
- stored mapped relations replace stale priority relation with `priority::urgent`;
- no `priority::none` is synthesized.

Add a second case for priority `none` asserting the payload contains ordinary/workflow labels but no priority mapping label. Keep the existing batch test and extend it with urgent if needed to ensure batch updates use the same canonical payload.

**Step 2: Run the focused handler tests and verify RED if a gap exists**

Run:

```bash
cd server && go test ./internal/handler -run 'Test(UpdateIssueGitlab_.*Priority|BatchUpdateIssuesGitlab_.*MappedFields)' -count=1
```

Expected: existing high/medium behavior may pass; the urgent case must fail until the mapping is implemented. If all cases pass after Task 2, retain the tests as regression coverage and document that the existing outbox path already satisfies the local-to-remote contract.

### Task 5: Add worker remote PUT and canonical response coverage

**Files:**
- Modify: `server/internal/gitlabsync/write_ops_test.go`

**Step 1: Add the worker test**

Seed an `update_issue` outbox row with labels `bug`, `workflow::in-review`, and `priority::urgent`. Use the existing httptest server and assert:

- exactly one PUT targets `/api/v4/projects/42/issues/7`;
- request labels preserve all three labels;
- response labels containing `priority::urgent` reach the canonical applier;
- the worker marks the outbox row successful.

Add a `none` request case asserting no priority mapping label is sent.

**Step 2: Run and verify**

Run:

```bash
cd server && go test ./internal/gitlabsync -run 'TestTick_UpdateIssueOp.*(Priority|Labels)' -count=1
```

Expected: PASS after Task 2; failure indicates a worker payload decoding or client response path gap that must be fixed at the smallest layer.

### Task 6: Update integration documentation

**Files:**
- Modify: `docs/rimedeck-feature-gitlab-integration.md`

**Step 1: Update stale statements**

Document that GitLab priority labels map bidirectionally for `urgent`, `high`, `medium`, and `low`; `none` removes priority mapping labels. State that local priority changes enqueue a full canonical label update and that ordinary/workflow labels are preserved. Keep the complete-relation versus visible-label distinction.

**Step 2: Review the diff for scope**

Ensure no claim says `priority::none` is generated or that GitLab has a native priority field in this flow.

### Task 7: Run final focused verification

**Files:**
- No source changes expected.

**Step 1: Run mapping tests**

```bash
cd server && go test ./internal/gitlabtracker -run 'Test(Classify|Project|Canonical)' -count=1
```

Expected: PASS.

**Step 2: Run client and worker sync tests**

```bash
cd server && go test ./internal/gitlabtracker ./internal/gitlabsync -count=1
```

Expected: PASS.

**Step 3: Run handler GitLab sync tests**

```bash
cd server && go test ./internal/handler -run 'Test(UpdateIssueGitlab|BatchUpdateIssuesGitlab|ImportIssues|ApplyCanonical)' -count=1
```

Expected: PASS, or database-backed tests skip only when the repository test database is unavailable.

**Step 4: Run the real behavioral smoke path**

Against the repository GitLab stub/test harness:

1. import an Issue with `priority::urgent`, `workflow::in-review`, and ordinary `bug`;
2. observe local `priority=urgent` and stored complete relations;
3. update only local priority to `none`, then drain the worker;
4. verify the remote PUT contains `bug` and `workflow::in-review` but no priority mapping label;
5. update local priority to `urgent`, drain again, and verify the remote PUT contains `priority::urgent`;
6. fetch/reconcile the Issue and verify local priority remains `urgent`.

Record exact command/output in the final report. Do not claim completion without fresh verification evidence.
