-- Reverse: sop → workflow

-- 1. Restore CHECK constraint
ALTER TABLE sop_run DROP CONSTRAINT IF EXISTS sop_run_source_check;
ALTER TABLE sop_run ADD CONSTRAINT workflow_run_source_check
    CHECK (source IN ('manual', 'autopilot', 'api', 'schedule', 'mention', 'chat'));

-- 2. Rename indexes back
ALTER INDEX idx_sop_workspace           RENAME TO idx_workflow_workspace;
ALTER INDEX idx_sop_run_sop             RENAME TO idx_wf_run_workflow;
ALTER INDEX idx_sop_run_workspace       RENAME TO idx_wf_run_workspace;
ALTER INDEX idx_sop_node_exec_run       RENAME TO idx_wf_node_exec_run;
ALTER INDEX idx_agent_sop_sop           RENAME TO idx_agent_workflow_workflow;
ALTER INDEX idx_agent_sop_agent         RENAME TO idx_agent_workflow_agent;
ALTER INDEX idx_sop_credential_workspace RENAME TO idx_wf_credential_workspace;
ALTER INDEX idx_sop_version_sop         RENAME TO idx_wf_version_workflow;

-- 3. Rename columns back: sop_id → workflow_id
ALTER TABLE sop_run     RENAME COLUMN sop_id TO workflow_id;
ALTER TABLE sop_version RENAME COLUMN sop_id TO workflow_id;
ALTER TABLE agent_sop   RENAME COLUMN sop_id TO workflow_id;
ALTER TABLE autopilot   RENAME COLUMN sop_id TO workflow_id;

-- 4. Rename tables back (order: root → leaf)
ALTER TABLE sop                RENAME TO workflow;
ALTER TABLE agent_sop          RENAME TO agent_workflow;
ALTER TABLE sop_run            RENAME TO workflow_run;
ALTER TABLE sop_credential     RENAME TO workflow_credential;
ALTER TABLE sop_version        RENAME TO workflow_version;
ALTER TABLE sop_node_execution RENAME TO workflow_node_execution;
