DROP INDEX IF EXISTS issue_label_visible_by_tracker_idx;

ALTER TABLE issue_label
  DROP COLUMN IF EXISTS mapping_kind;
