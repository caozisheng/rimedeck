CREATE TABLE sys_cron_executions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name       TEXT        NOT NULL,
    scope_kind     TEXT        NOT NULL DEFAULT 'global',
    scope_id       TEXT        NOT NULL DEFAULT 'global',
    plan_time      TIMESTAMPTZ NOT NULL,

    status         TEXT        NOT NULL,
    attempt        INTEGER     NOT NULL DEFAULT 1,
    max_attempts   INTEGER     NOT NULL DEFAULT 3,
    next_retry_at  TIMESTAMPTZ,

    runner_id      TEXT,
    lease_token    UUID        NOT NULL DEFAULT gen_random_uuid(),
    heartbeat_at   TIMESTAMPTZ,
    stale_after    TIMESTAMPTZ,

    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    duration_ms    INTEGER,
    rows_affected  BIGINT,
    result         JSONB       NOT NULL DEFAULT '{}'::jsonb,

    error_code     TEXT,
    error_msg      TEXT,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_sys_cron_status
        CHECK (status IN ('RUNNING', 'SUCCESS', 'FAILED')),
    CONSTRAINT chk_sys_cron_attempt
        CHECK (attempt >= 1 AND max_attempts >= attempt),
    CONSTRAINT chk_sys_cron_duration
        CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CONSTRAINT uq_sys_cron_execution
        UNIQUE (job_name, scope_kind, scope_id, plan_time)
);

CREATE INDEX idx_sys_cron_exec_job_plan
    ON sys_cron_executions (job_name, scope_kind, scope_id, plan_time DESC);

CREATE INDEX idx_sys_cron_exec_running_stale
    ON sys_cron_executions (stale_after)
    WHERE status = 'RUNNING';

CREATE INDEX idx_sys_cron_exec_failed_recent
    ON sys_cron_executions (job_name, plan_time DESC)
    WHERE status = 'FAILED';

CREATE INDEX idx_sys_cron_exec_finished
    ON sys_cron_executions (finished_at)
    WHERE status IN ('SUCCESS', 'FAILED');

-- Fast-forward the hourly rollup watermark so the scheduler does not
-- spend thousands of ticks crawling from 1970 to the present day.
-- GREATEST keeps an already-advanced watermark untouched.
UPDATE task_usage_hourly_rollup_state
   SET watermark_at = GREATEST(watermark_at, now() - interval '7 days')
 WHERE id = 1;
