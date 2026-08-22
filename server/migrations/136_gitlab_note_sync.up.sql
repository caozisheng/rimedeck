ALTER TABLE tracker_sync_outbox
  DROP CONSTRAINT tracker_sync_outbox_operation_check;

ALTER TABLE tracker_sync_outbox
  ADD CONSTRAINT tracker_sync_outbox_operation_check CHECK (operation IN (
    'create_issue','update_issue','delete_issue','set_labels',
    'create_note','update_note','delete_note','pull_notes',
    'pull_issue','pull_labels','reconcile','full_reconcile'
  ));

ALTER TABLE gitlab_issue_link
  ADD COLUMN notes_initialized_at TIMESTAMPTZ;

CREATE TABLE gitlab_note_link (
  comment_id              UUID PRIMARY KEY REFERENCES comment(id) ON DELETE CASCADE,
  issue_id                UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
  tracker_connection_id   UUID NOT NULL REFERENCES gitlab_tracker_connection(id) ON DELETE CASCADE,
  remote_issue_iid        INTEGER NOT NULL,
  remote_note_id          BIGINT NOT NULL,
  remote_author_id        BIGINT,
  remote_author_name      TEXT,
  remote_author_url       TEXT,
  remote_created_at       TIMESTAMPTZ NOT NULL,
  remote_updated_at       TIMESTAMPTZ NOT NULL,
  last_remote_body        TEXT NOT NULL,
  remote_owned            BOOLEAN NOT NULL DEFAULT true,
  last_pulled_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tracker_connection_id, remote_note_id)
);

CREATE INDEX idx_gitlab_note_link_issue ON gitlab_note_link(issue_id, remote_created_at);
