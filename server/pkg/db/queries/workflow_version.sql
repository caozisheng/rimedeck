-- name: CreateWorkflowVersion :one
INSERT INTO sop_version (sop_id, version, graph, published_by)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListWorkflowVersions :many
SELECT * FROM sop_version WHERE sop_id = $1 ORDER BY version DESC;

-- name: GetWorkflowVersion :one
SELECT * FROM sop_version WHERE sop_id = $1 AND version = $2;
