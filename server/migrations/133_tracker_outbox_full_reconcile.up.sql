-- Widen the tracker_sync_outbox operation whitelist to include the
-- scheduler's `full_reconcile` op (introduced in Phase 4 Task 3). The
-- original migration 128 CHECK only listed reconcile / pull_issue /
-- pull_labels / create_issue / update_issue / delete_issue / set_labels,
-- so every scheduler tick from Phase 4 forward tripped SQLSTATE 23514
-- ("new row for relation \"tracker_sync_outbox\" violates check
-- constraint \"tracker_sync_outbox_operation_check\"") and no
-- full-sweep reconcile ever landed. Drop and re-add so the constraint
-- name stays stable for anyone grep'ing prod logs.
ALTER TABLE tracker_sync_outbox
  DROP CONSTRAINT tracker_sync_outbox_operation_check;

ALTER TABLE tracker_sync_outbox
  ADD CONSTRAINT tracker_sync_outbox_operation_check CHECK (operation IN (
    'create_issue','update_issue','delete_issue','set_labels',
    'pull_issue','pull_labels','reconcile','full_reconcile'
  ));
