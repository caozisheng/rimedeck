-- Agentic Workflow: workflow definitions, execution runs, and per-node logs.

CREATE TABLE workflow (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    icon            TEXT NOT NULL DEFAULT '',
    category        TEXT NOT NULL DEFAULT 'general'
                    CHECK (category IN ('general', 'document', 'scraper', 'subscription', 'spreadsheet', 'sales')),

    -- RuleGo DSL JSON — directly passed to rulego.New(), zero conversion.
    graph           JSONB NOT NULL DEFAULT '{"ruleChain":{},"metadata":{"firstNodeIndex":0,"nodes":[],"connections":[]}}',

    status          TEXT NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft', 'published', 'archived')),
    version         INT NOT NULL DEFAULT 1,

    created_by      UUID REFERENCES "user"(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ,

    UNIQUE(workspace_id, name)
);

CREATE TABLE workflow_run (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id     UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL,
    agent_id        UUID REFERENCES agent(id) ON DELETE SET NULL,

    source          TEXT NOT NULL DEFAULT 'manual'
                    CHECK (source IN ('manual', 'autopilot', 'api', 'schedule', 'mention')),
    trigger_input   JSONB,

    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    total_nodes     INT NOT NULL DEFAULT 0,
    completed_nodes INT NOT NULL DEFAULT 0,
    current_node_id TEXT,

    output          JSONB,
    error           TEXT,

    issue_id        UUID REFERENCES issue(id) ON DELETE SET NULL,
    autopilot_run_id UUID,
    triggered_by    UUID REFERENCES "user"(id) ON DELETE SET NULL,

    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    total_tokens    BIGINT NOT NULL DEFAULT 0,
    total_cost      NUMERIC(12,6) NOT NULL DEFAULT 0
);

CREATE TABLE workflow_node_execution (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    node_id         TEXT NOT NULL,
    node_type       TEXT NOT NULL,

    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped')),
    inputs          JSONB,
    outputs         JSONB,
    error           TEXT,

    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    tokens_used     BIGINT NOT NULL DEFAULT 0,
    duration_ms     INT NOT NULL DEFAULT 0
);

-- Indexes
CREATE INDEX idx_workflow_workspace ON workflow(workspace_id, status);
CREATE INDEX idx_wf_run_workflow ON workflow_run(workflow_id, created_at DESC);
CREATE INDEX idx_wf_run_workspace ON workflow_run(workspace_id, status);
CREATE INDEX idx_wf_node_exec_run ON workflow_node_execution(run_id);
