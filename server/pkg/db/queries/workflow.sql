-- Workflow CRUD

-- name: ListWorkflowsByWorkspace :many
SELECT id, workspace_id, name, description, icon, category, status, version,
       created_by, created_at, updated_at, published_at
FROM workflow
WHERE workspace_id = $1
ORDER BY updated_at DESC;

-- name: ListWorkflowsByWorkspaceAndStatus :many
SELECT id, workspace_id, name, description, icon, category, status, version,
       created_by, created_at, updated_at, published_at
FROM workflow
WHERE workspace_id = $1 AND status = $2
ORDER BY updated_at DESC;

-- name: GetWorkflow :one
SELECT * FROM workflow WHERE id = $1;

-- name: GetWorkflowInWorkspace :one
SELECT * FROM workflow WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkflow :one
INSERT INTO workflow (workspace_id, name, description, icon, category, graph, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateWorkflow :one
UPDATE workflow SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    category = COALESCE(sqlc.narg('category'), category),
    graph = COALESCE(sqlc.narg('graph'), graph),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: PublishWorkflow :one
UPDATE workflow SET
    status = 'published',
    version = version + 1,
    published_at = now(),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveWorkflow :exec
UPDATE workflow SET status = 'archived', updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteWorkflow :exec
DELETE FROM workflow WHERE id = $1 AND workspace_id = $2;

-- Workflow Run

-- name: CreateWorkflowRun :one
INSERT INTO workflow_run (workflow_id, workspace_id, agent_id, source, trigger_input, status, total_nodes, triggered_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetWorkflowRun :one
SELECT * FROM workflow_run WHERE id = $1;

-- name: ListWorkflowRuns :many
SELECT * FROM workflow_run
WHERE workflow_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateWorkflowRunStatus :exec
UPDATE workflow_run SET
    status = $2,
    current_node_id = $3,
    completed_nodes = $4,
    output = $5,
    error = $6,
    started_at = COALESCE(started_at, CASE WHEN $2 = 'running' THEN now() ELSE NULL END),
    completed_at = CASE WHEN $2 IN ('completed', 'failed', 'cancelled') THEN now() ELSE NULL END,
    total_tokens = $7,
    total_cost = $8
WHERE id = $1;

-- name: CancelWorkflowRun :exec
UPDATE workflow_run SET status = 'cancelled', completed_at = now()
WHERE id = $1 AND status IN ('pending', 'running');

-- Workflow Node Execution

-- name: CreateNodeExecution :one
INSERT INTO workflow_node_execution (run_id, node_id, node_type, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateNodeExecution :exec
UPDATE workflow_node_execution SET
    status = $2,
    inputs = $3,
    outputs = $4,
    error = $5,
    started_at = COALESCE(started_at, CASE WHEN $2 = 'running' THEN now() ELSE NULL END),
    completed_at = CASE WHEN $2 IN ('completed', 'failed', 'skipped') THEN now() ELSE NULL END,
    tokens_used = $6,
    duration_ms = $7
WHERE id = $1;

-- name: ListNodeExecutionsByRun :many
SELECT * FROM workflow_node_execution
WHERE run_id = $1
ORDER BY started_at ASC NULLS LAST;

-- Agent-Workflow junction (mirrors agent_skill pattern)

-- name: ListAgentWorkflows :many
SELECT w.id, w.workspace_id, w.name, w.description, w.icon, w.category, w.status, w.version,
       w.created_by, w.created_at, w.updated_at, w.published_at
FROM workflow w
JOIN agent_workflow aw ON aw.workflow_id = w.id
WHERE aw.agent_id = $1
ORDER BY w.name ASC;

-- name: AddAgentWorkflow :exec
INSERT INTO agent_workflow (agent_id, workflow_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveAgentWorkflow :exec
DELETE FROM agent_workflow
WHERE agent_id = $1 AND workflow_id = $2;

-- name: RemoveAllAgentWorkflows :exec
DELETE FROM agent_workflow WHERE agent_id = $1;

-- name: ListAgentWorkflowsByWorkspace :many
SELECT aw.agent_id, w.id, w.name, w.description, w.icon, w.category, w.status
FROM agent_workflow aw
JOIN workflow w ON w.id = aw.workflow_id
WHERE w.workspace_id = $1
ORDER BY w.name ASC;

-- name: CountWorkflowRunsByWorkflow :one
SELECT count(*) FROM workflow_run WHERE workflow_id = $1;

-- name: GetWorkflowStats :one
SELECT
    count(*) as total_runs,
    count(*) FILTER (WHERE status = 'completed') as completed_runs,
    count(*) FILTER (WHERE status = 'failed') as failed_runs,
    coalesce(sum(total_tokens), 0) as total_tokens,
    coalesce(avg(EXTRACT(EPOCH FROM (completed_at - started_at)) * 1000)::int, 0) as avg_duration_ms,
    max(created_at) as last_run_at
FROM workflow_run
WHERE workflow_id = $1;
