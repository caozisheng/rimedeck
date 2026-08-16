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

-- name: CreateGitlabIssueLink :one
INSERT INTO gitlab_issue_link (
  issue_id, tracker_connection_id, remote_issue_id, remote_iid,
  remote_web_url, remote_state, remote_updated_at, remote_author_name, remote_author_url
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: CreateTrackerOutbox :one
INSERT INTO tracker_sync_outbox (
  workspace_id, tracker_connection_id, issue_id, operation,
  payload, idempotency_key, desired_revision, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,'pending') RETURNING *;

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
