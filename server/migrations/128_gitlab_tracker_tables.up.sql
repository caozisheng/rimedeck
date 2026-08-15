-- GitLab tracker integration (design §7.1/§7.2/§7.3).
-- Phase 1 creates schema only; no runtime writes to these tables yet
-- except outbox rows enqueued by CreateIssue for gitlab-source issues.

CREATE TABLE gitlab_tracker_connection (
  id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id                UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  workspace_id              UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  instance_url              TEXT NOT NULL,
  remote_project_id         BIGINT NOT NULL,
  path_with_namespace       TEXT NOT NULL,
  web_url                   TEXT NOT NULL,
  clone_url                 TEXT NOT NULL,
  default_branch            TEXT,
  token_ciphertext          BYTEA NOT NULL,
  token_key_version         SMALLINT NOT NULL,
  webhook_secret_ciphertext BYTEA NOT NULL,
  webhook_id                BIGINT,
  webhook_state             TEXT NOT NULL CHECK (webhook_state IN ('active','unavailable','error')),
  state                     TEXT NOT NULL CHECK (state IN ('active','degraded','disabled')),
  last_pull_at              TIMESTAMPTZ,
  last_full_reconcile_at    TIMESTAMPTZ,
  last_error_code           TEXT,
  last_error_at             TIMESTAMPTZ,
  created_by                UUID NOT NULL REFERENCES "user"(id),
  created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (project_id, instance_url, remote_project_id)
);

CREATE INDEX idx_gitlab_tracker_connection_workspace ON gitlab_tracker_connection(workspace_id);
CREATE INDEX idx_gitlab_tracker_connection_project ON gitlab_tracker_connection(project_id);

CREATE TABLE gitlab_issue_link (
  issue_id                UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
  tracker_connection_id   UUID NOT NULL REFERENCES gitlab_tracker_connection(id) ON DELETE CASCADE,
  remote_issue_id         BIGINT NOT NULL,
  remote_iid              INTEGER NOT NULL,
  remote_web_url          TEXT NOT NULL,
  remote_state            TEXT NOT NULL CHECK (remote_state IN ('opened','closed')),
  remote_updated_at       TIMESTAMPTZ NOT NULL,
  remote_author_name      TEXT,
  remote_author_url       TEXT,
  remote_position         INTEGER,
  last_remote_snapshot    JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_pulled_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_pushed_at          TIMESTAMPTZ,
  UNIQUE (tracker_connection_id, remote_issue_id),
  UNIQUE (tracker_connection_id, remote_iid)
);

CREATE INDEX idx_gitlab_issue_link_connection ON gitlab_issue_link(tracker_connection_id);

CREATE TABLE tracker_sync_outbox (
  id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id            UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  tracker_connection_id   UUID NOT NULL REFERENCES gitlab_tracker_connection(id) ON DELETE CASCADE,
  issue_id                UUID REFERENCES issue(id) ON DELETE CASCADE,
  operation               TEXT NOT NULL CHECK (operation IN (
    'create_issue','update_issue','delete_issue','set_labels',
    'pull_issue','pull_labels','reconcile'
  )),
  payload                 JSONB NOT NULL,
  idempotency_key         UUID NOT NULL,
  status                  TEXT NOT NULL CHECK (status IN ('pending','running','retrying','failed','succeeded','cancelled')),
  attempts                INTEGER NOT NULL DEFAULT 0,
  available_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  desired_revision        BIGINT,
  last_error_code         TEXT,
  last_error_message      TEXT,
  created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tracker_connection_id, idempotency_key)
);

CREATE INDEX idx_tracker_outbox_ready
  ON tracker_sync_outbox (available_at, created_at)
  WHERE status IN ('pending','retrying');
CREATE INDEX idx_tracker_outbox_issue ON tracker_sync_outbox(issue_id)
  WHERE issue_id IS NOT NULL;
