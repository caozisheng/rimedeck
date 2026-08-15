DROP INDEX IF EXISTS idx_tracker_outbox_issue;
DROP INDEX IF EXISTS idx_tracker_outbox_ready;
DROP TABLE IF EXISTS tracker_sync_outbox;
DROP INDEX IF EXISTS idx_gitlab_issue_link_connection;
DROP TABLE IF EXISTS gitlab_issue_link;
DROP INDEX IF EXISTS idx_gitlab_tracker_connection_project;
DROP INDEX IF EXISTS idx_gitlab_tracker_connection_workspace;
DROP TABLE IF EXISTS gitlab_tracker_connection;
