-- last_webhook_at surfaces on the health endpoint so operators can tell
-- at a glance whether GitLab is actually delivering hooks. Nullable
-- because pre-Phase-4 connections don't have a value yet — the UI
-- treats null as "never".
ALTER TABLE gitlab_tracker_connection
  ADD COLUMN IF NOT EXISTS last_webhook_at TIMESTAMPTZ;
