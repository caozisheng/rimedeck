CREATE TABLE workflow_credential (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    credential_type TEXT NOT NULL DEFAULT 'api_key'
                    CHECK (credential_type IN ('api_key', 'bearer_token', 'basic_auth', 'custom_header')),
    value           JSONB NOT NULL DEFAULT '{}',
    created_by      UUID REFERENCES "user"(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name)
);

CREATE INDEX idx_wf_credential_workspace ON workflow_credential(workspace_id);
