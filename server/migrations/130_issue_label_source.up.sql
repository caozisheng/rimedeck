-- Preserve local label behavior while adding provider identity for GitLab labels.
ALTER TABLE issue_label
  ADD COLUMN source_type TEXT NOT NULL DEFAULT 'local'
    CHECK (source_type IN ('local','gitlab')),
  ADD COLUMN gitlab_tracker_connection_id UUID
    REFERENCES gitlab_tracker_connection(id) ON DELETE CASCADE,
  ADD COLUMN gitlab_label_id BIGINT,
  ADD COLUMN is_project_label BOOLEAN NOT NULL DEFAULT true,
  ADD COLUMN is_archived BOOLEAN NOT NULL DEFAULT false;

DROP INDEX IF EXISTS issue_label_workspace_name_lower_idx;
CREATE UNIQUE INDEX issue_label_local_name_uniq
  ON issue_label (workspace_id, LOWER(name)) WHERE source_type = 'local';
CREATE UNIQUE INDEX issue_label_gitlab_identity_uniq
  ON issue_label (gitlab_tracker_connection_id, gitlab_label_id)
  WHERE source_type = 'gitlab';
