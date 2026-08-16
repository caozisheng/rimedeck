-- Revert to the migration 128 whitelist.
ALTER TABLE tracker_sync_outbox
  DROP CONSTRAINT tracker_sync_outbox_operation_check;

ALTER TABLE tracker_sync_outbox
  ADD CONSTRAINT tracker_sync_outbox_operation_check CHECK (operation IN (
    'create_issue','update_issue','delete_issue','set_labels',
    'pull_issue','pull_labels','reconcile'
  ));
