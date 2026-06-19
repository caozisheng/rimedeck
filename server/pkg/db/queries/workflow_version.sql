-- name: CreateWorkflowVersion :one
INSERT INTO workflow_version (workflow_id, version, graph, published_by)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListWorkflowVersions :many
SELECT * FROM workflow_version WHERE workflow_id = $1 ORDER BY version DESC;

-- name: GetWorkflowVersion :one
SELECT * FROM workflow_version WHERE workflow_id = $1 AND version = $2;
