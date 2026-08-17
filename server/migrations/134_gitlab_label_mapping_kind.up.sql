ALTER TABLE issue_label
  ADD COLUMN mapping_kind TEXT NOT NULL DEFAULT 'none'
    CHECK (mapping_kind IN ('none', 'workflow', 'priority'));

UPDATE issue_label
SET mapping_kind = CASE LOWER(name)
  WHEN 'workflow::backlog' THEN 'workflow'
  WHEN 'workflow::todo' THEN 'workflow'
  WHEN 'workflow::in-progress' THEN 'workflow'
  WHEN 'workflow::in-review' THEN 'workflow'
  WHEN 'workflow::done' THEN 'workflow'
  WHEN 'priority::high' THEN 'priority'
  WHEN 'priority::medium' THEN 'priority'
  WHEN 'priority::low' THEN 'priority'
  ELSE 'none'
END
WHERE source_type = 'gitlab';

CREATE INDEX issue_label_visible_by_tracker_idx
  ON issue_label (gitlab_tracker_connection_id, LOWER(name))
  WHERE source_type = 'gitlab'
    AND mapping_kind = 'none'
    AND is_archived = false;
