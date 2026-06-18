-- name: CreateIssueDependency :one
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type, workspace_id, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteIssueDependency :exec
DELETE FROM issue_dependency
WHERE id = $1 AND workspace_id = $2;

-- name: GetIssueDependency :one
SELECT * FROM issue_dependency
WHERE id = $1;

-- name: ListDependenciesByIssue :many
SELECT * FROM issue_dependency
WHERE (issue_id = $1 OR depends_on_issue_id = $1)
  AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListDependenciesByProject :many
SELECT d.* FROM issue_dependency d
JOIN issue i1 ON d.issue_id = i1.id
JOIN issue i2 ON d.depends_on_issue_id = i2.id
WHERE i1.project_id = $1 AND i2.project_id = $1
  AND d.workspace_id = $2
ORDER BY d.created_at ASC;

-- name: ListDependenciesByParent :many
SELECT d.* FROM issue_dependency d
JOIN issue i1 ON d.issue_id = i1.id
JOIN issue i2 ON d.depends_on_issue_id = i2.id
WHERE i1.parent_issue_id = $1 AND i2.parent_issue_id = $1
  AND d.workspace_id = $2
ORDER BY d.created_at ASC;

-- name: ListBlockDependenciesFromIssue :many
SELECT depends_on_issue_id FROM issue_dependency
WHERE issue_id = $1 AND type = 'blocks';

-- name: DeleteDependenciesByIssue :exec
DELETE FROM issue_dependency
WHERE issue_id = $1 OR depends_on_issue_id = $1;
