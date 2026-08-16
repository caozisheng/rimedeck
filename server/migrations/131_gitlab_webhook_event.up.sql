-- gitlab_webhook_event dedupes GitLab webhook deliveries by
-- (tracker_connection_id, event_uuid). GitLab retries on 5xx/timeouts
-- with the same X-Gitlab-Event-UUID, so a hot-loop of retries would
-- otherwise fan-out multiple outbox rows for the same remote change.
--
-- received_at is indexed independently so a periodic GC (e.g. delete
-- rows older than 30 days) can prune the table without a full scan.
CREATE TABLE gitlab_webhook_event (
  tracker_connection_id UUID NOT NULL REFERENCES gitlab_tracker_connection(id) ON DELETE CASCADE,
  event_uuid            TEXT NOT NULL,
  received_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tracker_connection_id, event_uuid)
);

CREATE INDEX idx_gitlab_webhook_event_received_at
  ON gitlab_webhook_event (received_at);
