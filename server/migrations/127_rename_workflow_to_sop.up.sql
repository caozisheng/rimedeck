-- Rename workflow → sop across all tables, columns, indexes, and constraints.
-- Tables 122-126 are already applied; this migration renames in place.

-- 1. Rename tables (order: leaf → root to avoid FK conflicts)
ALTER TABLE workflow_node_execution RENAME TO sop_node_execution;
ALTER TABLE workflow_version        RENAME TO sop_version;
ALTER TABLE workflow_credential     RENAME TO sop_credential;
ALTER TABLE workflow_run            RENAME TO sop_run;
ALTER TABLE agent_workflow          RENAME TO agent_sop;
ALTER TABLE workflow                RENAME TO sop;

-- 2. Rename columns: workflow_id → sop_id
ALTER TABLE sop_run     RENAME COLUMN workflow_id TO sop_id;
ALTER TABLE sop_version RENAME COLUMN workflow_id TO sop_id;
ALTER TABLE agent_sop   RENAME COLUMN workflow_id TO sop_id;
ALTER TABLE autopilot   RENAME COLUMN workflow_id TO sop_id;

-- 3. Rename indexes
ALTER INDEX idx_workflow_workspace      RENAME TO idx_sop_workspace;
ALTER INDEX idx_wf_run_workflow         RENAME TO idx_sop_run_sop;
ALTER INDEX idx_wf_run_workspace        RENAME TO idx_sop_run_workspace;
ALTER INDEX idx_wf_node_exec_run        RENAME TO idx_sop_node_exec_run;
ALTER INDEX idx_agent_workflow_workflow  RENAME TO idx_agent_sop_sop;
ALTER INDEX idx_agent_workflow_agent    RENAME TO idx_agent_sop_agent;
ALTER INDEX idx_wf_credential_workspace RENAME TO idx_sop_credential_workspace;
ALTER INDEX idx_wf_version_workflow     RENAME TO idx_sop_version_sop;

-- 4. Rename CHECK constraint + add 'mcp' source
ALTER TABLE sop_run DROP CONSTRAINT IF EXISTS workflow_run_source_check;
ALTER TABLE sop_run ADD CONSTRAINT sop_run_source_check
    CHECK (source IN ('manual', 'autopilot', 'api', 'schedule', 'mention', 'chat', 'mcp'));
