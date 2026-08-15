DROP INDEX IF EXISTS idx_issue_tracker_connection;
DROP INDEX IF EXISTS idx_issue_project_source;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_source_connection_check;
ALTER TABLE issue
  DROP COLUMN IF EXISTS synced_revision,
  DROP COLUMN IF EXISTS sync_revision,
  DROP COLUMN IF EXISTS sync_state,
  DROP COLUMN IF EXISTS tracker_connection_id,
  DROP COLUMN IF EXISTS source_type;
