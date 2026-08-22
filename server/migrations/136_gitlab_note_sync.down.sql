DROP TABLE IF EXISTS gitlab_note_link;

ALTER TABLE gitlab_issue_link
  DROP COLUMN IF EXISTS notes_initialized_at;

ALTER TABLE tracker_sync_outbox
  DROP CONSTRAINT tracker_sync_outbox_operation_check;

ALTER TABLE tracker_sync_outbox
  ADD CONSTRAINT tracker_sync_outbox_operation_check CHECK (operation IN (
    'create_issue','update_issue','delete_issue','set_labels',
    'pull_issue','pull_labels','reconcile','full_reconcile'
  ));
