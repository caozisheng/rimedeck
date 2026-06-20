ALTER TABLE workflow_run DROP CONSTRAINT IF EXISTS workflow_run_source_check;
ALTER TABLE workflow_run ADD CONSTRAINT workflow_run_source_check
    CHECK (source IN ('manual', 'autopilot', 'api', 'schedule', 'mention'));
