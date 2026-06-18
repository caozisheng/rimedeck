-- Enhance the issue_dependency table for the DAG/flowchart view feature.
-- Adds workspace_id, created_by, created_at, a self-reference guard,
-- a uniqueness constraint, and tighter type values.

-- 1. Add workspace_id as nullable first, backfill, then set NOT NULL.
ALTER TABLE issue_dependency
    ADD COLUMN workspace_id UUID;

UPDATE issue_dependency
    SET workspace_id = (SELECT workspace_id FROM issue WHERE issue.id = issue_dependency.issue_id);

ALTER TABLE issue_dependency
    ALTER COLUMN workspace_id SET NOT NULL;

ALTER TABLE issue_dependency
    ADD CONSTRAINT fk_issue_dep_workspace
    FOREIGN KEY (workspace_id) REFERENCES workspace(id);

-- 2. Add created_by (nullable — may not always be known).
ALTER TABLE issue_dependency
    ADD COLUMN created_by UUID;

-- 3. Add created_at with a default.
ALTER TABLE issue_dependency
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- 4. Migrate type values before swapping the CHECK constraint.
--    'related' -> 'relates_to'; drop 'blocked_by' rows (direction is
--    encoded by which column is issue_id vs depends_on_issue_id).
UPDATE issue_dependency SET type = 'relates_to' WHERE type = 'related';
DELETE FROM issue_dependency WHERE type = 'blocked_by';

-- 5. Drop old CHECK on type, add new one.
ALTER TABLE issue_dependency
    DROP CONSTRAINT issue_dependency_type_check;

ALTER TABLE issue_dependency
    ADD CONSTRAINT issue_dependency_type_check
    CHECK (type IN ('blocks', 'relates_to'));

-- 6. Self-reference guard.
ALTER TABLE issue_dependency
    ADD CONSTRAINT chk_issue_dep_no_self_ref
    CHECK (issue_id != depends_on_issue_id);

-- 7. Unique constraint on the directed edge.
ALTER TABLE issue_dependency
    ADD CONSTRAINT uq_issue_dep_edge
    UNIQUE (issue_id, depends_on_issue_id);

-- 8. Indexes for lookup by either side and by workspace.
CREATE INDEX idx_issue_dep_from ON issue_dependency(issue_id);
CREATE INDEX idx_issue_dep_to   ON issue_dependency(depends_on_issue_id);
CREATE INDEX idx_issue_dep_ws   ON issue_dependency(workspace_id);
