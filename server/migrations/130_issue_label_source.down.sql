DROP INDEX IF EXISTS issue_label_local_name_uniq;
DROP INDEX IF EXISTS issue_label_gitlab_identity_uniq;
CREATE UNIQUE INDEX issue_label_workspace_name_lower_idx
  ON issue_label (workspace_id, LOWER(name));
ALTER TABLE issue_label
  DROP COLUMN IF EXISTS is_archived,
  DROP COLUMN IF EXISTS is_project_label,
  DROP COLUMN IF EXISTS gitlab_label_id,
  DROP COLUMN IF EXISTS gitlab_tracker_connection_id,
  DROP COLUMN IF EXISTS source_type;
