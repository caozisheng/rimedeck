-- Agent-Workflow junction (mirrors agent_skill pattern) and Autopilot workflow column.

CREATE TABLE agent_workflow (
    agent_id    UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, workflow_id)
);

CREATE INDEX idx_agent_workflow_workflow ON agent_workflow(workflow_id);
CREATE INDEX idx_agent_workflow_agent ON agent_workflow(agent_id);

-- Autopilot can optionally reference a workflow to execute instead of coding.
ALTER TABLE autopilot ADD COLUMN workflow_id UUID REFERENCES workflow(id) ON DELETE SET NULL;
