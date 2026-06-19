CREATE TABLE workflow_version (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id  UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    version      INT NOT NULL,
    graph        JSONB NOT NULL,
    published_by UUID REFERENCES "user"(id),
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workflow_id, version)
);

CREATE INDEX idx_wf_version_workflow ON workflow_version(workflow_id, version DESC);
