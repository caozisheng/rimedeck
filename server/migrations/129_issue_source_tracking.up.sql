-- Issue source & sync projection (design §7.2). All defaults keep existing
-- rows on the local fast path: source_type='local', no tracker, sync_state='local'.
ALTER TABLE issue
  ADD COLUMN source_type TEXT NOT NULL DEFAULT 'local'
    CHECK (source_type IN ('local','gitlab','detached')),
  ADD COLUMN tracker_connection_id UUID
    REFERENCES gitlab_tracker_connection(id) ON DELETE SET NULL,
  ADD COLUMN sync_state TEXT NOT NULL DEFAULT 'local'
    CHECK (sync_state IN ('local','pending','syncing','synced','failed','pending_delete','detached')),
  ADD COLUMN sync_revision BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN synced_revision BIGINT NOT NULL DEFAULT 0;

ALTER TABLE issue
  ADD CONSTRAINT issue_source_connection_check CHECK (
    (source_type = 'local' AND tracker_connection_id IS NULL)
    OR (source_type = 'gitlab' AND tracker_connection_id IS NOT NULL)
    OR source_type = 'detached'
  );

CREATE INDEX idx_issue_project_source ON issue(project_id, source_type);
CREATE INDEX idx_issue_tracker_connection ON issue(tracker_connection_id)
  WHERE tracker_connection_id IS NOT NULL;
