-- name: GetGitlabTrackerConnection :one
SELECT * FROM gitlab_tracker_connection WHERE id = $1;

-- name: GetGitlabTrackerConnectionInWorkspace :one
SELECT * FROM gitlab_tracker_connection WHERE id = $1 AND workspace_id = $2;

-- name: ListGitlabTrackerConnectionsByProject :many
SELECT * FROM gitlab_tracker_connection
WHERE project_id = $1 AND state <> 'disabled'
ORDER BY created_at ASC;

-- name: CreateGitlabTrackerConnection :one
INSERT INTO gitlab_tracker_connection (
  project_id, workspace_id, instance_url, remote_project_id, path_with_namespace,
  web_url, clone_url, default_branch, token_ciphertext, token_key_version,
  webhook_secret_ciphertext, webhook_state, state, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'active',$13) RETURNING *;

-- name: GetGitlabIssueLinkByIssueID :one
SELECT * FROM gitlab_issue_link WHERE issue_id = $1;

-- name: ListGitlabIssueLinksByIssues :many
SELECT l.*, c.instance_url AS connection_instance_url
FROM gitlab_issue_link l
JOIN gitlab_tracker_connection c ON c.id = l.tracker_connection_id
WHERE l.issue_id = ANY($1::uuid[]);

-- name: GetGitlabIssueLinkByRemoteIID :one
SELECT * FROM gitlab_issue_link
WHERE tracker_connection_id = $1 AND remote_iid = $2;

-- name: MarkGitlabIssueNotesInitialized :exec
UPDATE gitlab_issue_link
SET notes_initialized_at = COALESCE(notes_initialized_at, now())
WHERE issue_id = $1;

-- name: CreateGitlabIssueLink :one
INSERT INTO gitlab_issue_link (
  issue_id, tracker_connection_id, remote_issue_id, remote_iid,
  remote_web_url, remote_state, remote_updated_at, remote_author_name, remote_author_url
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetGitlabNoteLinkByCommentID :one
SELECT * FROM gitlab_note_link WHERE comment_id = $1;

-- name: GetGitlabNoteLinkByRemoteID :one
SELECT * FROM gitlab_note_link
WHERE tracker_connection_id = $1 AND remote_note_id = $2;

-- name: ListGitlabNoteLinksByComments :many
SELECT * FROM gitlab_note_link WHERE comment_id = ANY($1::uuid[]);

-- name: ListGitlabNoteLinksByIssue :many
SELECT * FROM gitlab_note_link
WHERE issue_id = $1
ORDER BY remote_created_at, remote_note_id;

-- name: UpsertGitlabNoteLink :one
INSERT INTO gitlab_note_link (
  comment_id, issue_id, tracker_connection_id, remote_issue_iid, remote_note_id,
  remote_author_id, remote_author_name, remote_author_url,
  remote_created_at, remote_updated_at, last_remote_body, remote_owned, last_pulled_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())
ON CONFLICT (tracker_connection_id, remote_note_id) DO UPDATE SET
  remote_author_id = EXCLUDED.remote_author_id,
  remote_author_name = EXCLUDED.remote_author_name,
  remote_author_url = EXCLUDED.remote_author_url,
  remote_updated_at = EXCLUDED.remote_updated_at,
  last_remote_body = EXCLUDED.last_remote_body,
  last_pulled_at = now()
RETURNING *;

-- name: DeleteGitlabNoteLinkByCommentID :exec
DELETE FROM gitlab_note_link WHERE comment_id = $1;

-- name: DeleteGitlabNoteLinkByRemoteID :exec
DELETE FROM gitlab_note_link
WHERE tracker_connection_id = $1 AND remote_note_id = $2;

-- name: CreateTrackerOutbox :one
INSERT INTO tracker_sync_outbox (
  workspace_id, tracker_connection_id, issue_id, operation,
  payload, idempotency_key, desired_revision, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,'pending') RETURNING *;

-- name: EnqueueScheduledTrackerOutbox :exec
-- Serialize scheduler enqueue per tracker/operation, cancel duplicate queued
-- pulls, and insert only when no pending/retrying row already exists.
WITH lock AS (
  SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('tracker_id')::text || ':' || sqlc.arg('operation')::text, 0))
),
queued AS (
  SELECT o.id, row_number() OVER (ORDER BY o.available_at, o.created_at) AS rn
  FROM tracker_sync_outbox o, lock
  WHERE o.tracker_connection_id = sqlc.arg('tracker_id')::uuid
    AND o.operation = sqlc.arg('operation')::text
    AND o.status IN ('pending','retrying')
), cancelled AS (
  UPDATE tracker_sync_outbox o
  SET status = 'cancelled', updated_at = now()
  FROM queued q
  WHERE o.id = q.id AND q.rn > 1
  RETURNING o.id
)
INSERT INTO tracker_sync_outbox (
  workspace_id, tracker_connection_id, issue_id, operation,
  payload, idempotency_key, desired_revision, status
)
SELECT sqlc.arg('workspace_id')::uuid, sqlc.arg('tracker_id')::uuid, NULL::uuid,
       sqlc.arg('operation')::text, sqlc.arg('payload')::jsonb,
       sqlc.arg('idempotency_key')::uuid, NULL::bigint, 'pending'
WHERE NOT EXISTS (SELECT 1 FROM queued WHERE rn = 1);

-- name: CountTrackerOutboxByStatus :many
SELECT status, count(*)::bigint AS cnt
FROM tracker_sync_outbox
WHERE tracker_connection_id = $1
  AND status IN ('pending','running','retrying','failed')
GROUP BY status;

-- name: CancelTrackerOutboxByIssue :exec
UPDATE tracker_sync_outbox
SET status = 'cancelled', updated_at = now()
WHERE issue_id = $1 AND status IN ('pending','running','retrying');

-- name: UpdateIssueSyncState :exec
UPDATE issue SET sync_state = $2 WHERE id = $1;

-- name: BumpIssueSyncRevision :one
UPDATE issue
SET sync_revision = sync_revision + 1,
    sync_state = CASE WHEN $2::text = 'keep' THEN 'pending' ELSE $2::text END
WHERE id = $1
RETURNING sync_revision;

-- name: UpdateGitlabTrackerToken :one
UPDATE gitlab_tracker_connection
SET token_ciphertext = $2,
    token_key_version = $3,
    state = CASE WHEN state = 'disabled' THEN 'disabled' ELSE 'active' END,
    last_error_code = NULL,
    last_error_at = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DisableGitlabTrackerConnection :one
UPDATE gitlab_tracker_connection
SET state = 'disabled',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteGitlabTrackerConnection :exec
DELETE FROM gitlab_tracker_connection WHERE id = $1;

-- name: DetachIssuesFromTracker :exec
UPDATE issue
SET source_type = 'detached',
    sync_state = 'detached',
    tracker_connection_id = NULL
WHERE tracker_connection_id = $1;

-- name: DeleteMirroredIssuesForTracker :exec
DELETE FROM issue WHERE tracker_connection_id = $1 AND source_type = 'gitlab';

-- name: ResetFailedTrackerOutbox :execrows
UPDATE tracker_sync_outbox
SET status = 'pending',
    available_at = now(),
    last_error_code = NULL,
    last_error_message = NULL,
    updated_at = now()
WHERE tracker_connection_id = $1 AND status = 'failed';

-- name: CountNonTerminalTrackerOutbox :one
SELECT count(*) FROM tracker_sync_outbox
WHERE tracker_connection_id = $1
  AND status IN ('pending','running','retrying');

-- name: CompressPendingTrackerOutbox :exec
-- Cancels older pending/retrying rows for the same
-- (tracker_connection_id, issue_id, operation) group whose
-- desired_revision is strictly less than the newest one just enqueued.
-- Called right after CreateTrackerOutbox so only the latest desired
-- state ever gets pushed. Rows already in flight (running) or terminal
-- (failed/succeeded/cancelled) are never touched — they own the wire.
UPDATE tracker_sync_outbox
SET status = 'cancelled', updated_at = now()
WHERE tracker_connection_id = $1
  AND issue_id = $2
  AND operation = $3
  AND status IN ('pending','retrying')
  AND desired_revision IS NOT NULL
  AND desired_revision < $4;

-- name: ClaimReadyTrackerOutbox :many
-- Selects up to $1 ready outbox rows and atomically flips them to
-- 'running' with an incremented attempt count. User writes are ordered
-- ahead of scheduler pulls so a reconcile backlog cannot starve local
-- edits or comment deletes.
WITH candidates AS (
  SELECT id, tracker_connection_id, available_at, created_at,
         CASE WHEN operation IN ('create_issue','update_issue','delete_issue','set_labels','create_note','update_note','delete_note') THEN 0 ELSE 1 END AS queue_class
  FROM tracker_sync_outbox
  WHERE status IN ('pending','retrying')
    AND available_at <= now()
),
per_connection AS (
  SELECT DISTINCT ON (tracker_connection_id) id
  FROM candidates
  ORDER BY tracker_connection_id, queue_class, available_at, created_at
),
ready AS (
  SELECT o.id, c.queue_class
  FROM tracker_sync_outbox o
  JOIN candidates c USING (id)
  JOIN per_connection pc USING (id)
  ORDER BY c.queue_class, o.available_at, o.created_at
  LIMIT $1
  FOR UPDATE OF o SKIP LOCKED
)
UPDATE tracker_sync_outbox o
SET status = 'running', attempts = attempts + 1, updated_at = now()
FROM ready
WHERE o.id = ready.id
RETURNING o.*;

-- name: MarkTrackerOutboxSucceeded :exec
UPDATE tracker_sync_outbox
SET status = 'succeeded',
    last_error_code = NULL,
    last_error_message = NULL,
    updated_at = now()
WHERE id = $1;

-- name: MarkTrackerOutboxRetry :exec
-- Pushes the row back into 'retrying' with a delayed available_at
-- (caller computes backoff based on attempts). Bumps error diagnostics.
UPDATE tracker_sync_outbox
SET status = 'retrying',
    available_at = $2,
    last_error_code = $3,
    last_error_message = $4,
    updated_at = now()
WHERE id = $1;

-- name: MarkTrackerOutboxFailed :exec
-- Terminal failure: attempts exhausted or non-retryable error.
UPDATE tracker_sync_outbox
SET status = 'failed',
    last_error_code = $2,
    last_error_message = $3,
    updated_at = now()
WHERE id = $1;

-- name: TouchTrackerLastPull :exec
UPDATE gitlab_tracker_connection
SET last_pull_at = now(), updated_at = now()
WHERE id = $1;

-- name: DetachSingleIssueFromTracker :exec
-- Turns one mirrored issue into a local-only record. Used by the
-- conflict dialog's "Convert to local" action so the user can escape
-- a wedged push without losing local edits.
UPDATE issue
SET source_type = 'detached',
    sync_state = 'detached',
    tracker_connection_id = NULL
WHERE id = $1 AND source_type = 'gitlab';

-- name: DiscardPendingIssueRevision :exec
-- Rolls the local sync_revision back to synced_revision so the next
-- canonical pull is authoritative. Callers pair this with
-- CancelTrackerOutboxByIssue to drop the queued push in the same tx.
UPDATE issue
SET sync_revision = synced_revision,
    sync_state = 'synced'
WHERE id = $1 AND source_type = 'gitlab';

-- name: InsertGitlabWebhookEvent :one
-- Idempotent record of a GitLab webhook delivery. Returns true when this
-- (tracker, event_uuid) pair is new — false marks a duplicate delivery
-- that the handler should ack with 200 but otherwise ignore.
INSERT INTO gitlab_webhook_event (tracker_connection_id, event_uuid)
VALUES ($1, $2)
ON CONFLICT (tracker_connection_id, event_uuid) DO NOTHING
RETURNING (xmax = 0) AS inserted;

-- name: SetTrackerWebhookProvisioned :exec
UPDATE gitlab_tracker_connection
SET webhook_id = $2,
    webhook_state = 'active',
    updated_at = now()
WHERE id = $1;

-- name: SetTrackerWebhookState :exec
UPDATE gitlab_tracker_connection
SET webhook_state = $2,
    updated_at = now()
WHERE id = $1;

-- name: ListGitlabIssueLinkIIDs :many
-- Returns every (issue_id, remote_iid) pair for a connection. Full
-- reconcile compares this against the current remote iid set to
-- detect out-of-band GitLab deletes.
SELECT issue_id, remote_iid
FROM gitlab_issue_link
WHERE tracker_connection_id = $1
ORDER BY remote_iid;

-- name: TouchLastFullReconcile :exec
UPDATE gitlab_tracker_connection
SET last_full_reconcile_at = now(), updated_at = now()
WHERE id = $1;

-- name: ListActiveTrackersForReconcile :many
-- Returns connections that need a periodic pull. Skips disabled so
-- the scheduler doesn't fight the disable flag. Bounded by the
-- caller-provided cutoffs.
SELECT id, project_id, workspace_id, last_pull_at, last_full_reconcile_at
FROM gitlab_tracker_connection
WHERE state <> 'disabled';

-- name: MarkTrackerDegraded :exec
-- Called by the worker after MaxAttempts retries or a terminal auth
-- error. state='disabled' is left alone — the operator disable flag
-- takes precedence over degradation heuristics.
UPDATE gitlab_tracker_connection
SET state = CASE WHEN state = 'disabled' THEN 'disabled' ELSE 'degraded' END,
    last_error_code = $2,
    last_error_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: MarkTrackerActive :exec
-- Successful op → clear the degradation flag and error markers.
UPDATE gitlab_tracker_connection
SET state = CASE WHEN state = 'degraded' THEN 'active' ELSE state END,
    last_error_code = NULL,
    last_error_at = NULL,
    updated_at = now()
WHERE id = $1;

-- name: TouchLastWebhook :exec
UPDATE gitlab_tracker_connection
SET last_webhook_at = now(), updated_at = now()
WHERE id = $1;

-- name: GetGitlabTrackerHealth :one
SELECT
  gtc.id, gtc.state, gtc.webhook_state,
  gtc.last_pull_at, gtc.last_full_reconcile_at, gtc.last_webhook_at,
  gtc.last_error_code,
  COALESCE(SUM(CASE WHEN o.status = 'pending' THEN 1 ELSE 0 END), 0)::bigint AS pending_count,
  COALESCE(SUM(CASE WHEN o.status = 'retrying' THEN 1 ELSE 0 END), 0)::bigint AS retrying_count,
  COALESCE(SUM(CASE WHEN o.status = 'failed' THEN 1 ELSE 0 END), 0)::bigint AS failed_count
FROM gitlab_tracker_connection gtc
LEFT JOIN tracker_sync_outbox o ON o.tracker_connection_id = gtc.id
WHERE gtc.id = $1
GROUP BY gtc.id;
