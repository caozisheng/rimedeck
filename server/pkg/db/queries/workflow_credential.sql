-- name: ListWorkflowCredentials :many
SELECT id, workspace_id, name, description, credential_type, created_by, created_at, updated_at
FROM workflow_credential WHERE workspace_id = $1 ORDER BY name ASC;

-- name: GetWorkflowCredential :one
SELECT * FROM workflow_credential WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkflowCredential :one
INSERT INTO workflow_credential (workspace_id, name, description, credential_type, value, created_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: UpdateWorkflowCredential :one
UPDATE workflow_credential SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    value = COALESCE(sqlc.narg('value'), value),
    updated_at = now()
WHERE id = $1 RETURNING *;

-- name: DeleteWorkflowCredential :exec
DELETE FROM workflow_credential WHERE id = $1 AND workspace_id = $2;
