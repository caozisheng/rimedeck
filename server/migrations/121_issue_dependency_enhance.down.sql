-- Reverse 121_issue_dependency_enhance.up.sql

-- Drop indexes.
DROP INDEX IF EXISTS idx_issue_dep_from;
DROP INDEX IF EXISTS idx_issue_dep_to;
DROP INDEX IF EXISTS idx_issue_dep_ws;

-- Drop unique constraint.
ALTER TABLE issue_dependency
    DROP CONSTRAINT IF EXISTS uq_issue_dep_edge;

-- Drop self-reference guard.
ALTER TABLE issue_dependency
    DROP CONSTRAINT IF EXISTS chk_issue_dep_no_self_ref;

-- Restore original type CHECK: drop the new one, rename data, then add old CHECK.
ALTER TABLE issue_dependency
    DROP CONSTRAINT issue_dependency_type_check;

-- Rename 'relates_to' back to 'related' BEFORE restoring the old CHECK.
UPDATE issue_dependency SET type = 'related' WHERE type = 'relates_to';

ALTER TABLE issue_dependency
    ADD CONSTRAINT issue_dependency_type_check
    CHECK (type IN ('blocks', 'blocked_by', 'related'));

-- Drop added columns (reverse order of addition).
ALTER TABLE issue_dependency DROP COLUMN created_at;
ALTER TABLE issue_dependency DROP COLUMN created_by;

-- Drop workspace FK then column.
ALTER TABLE issue_dependency
    DROP CONSTRAINT IF EXISTS fk_issue_dep_workspace;

ALTER TABLE issue_dependency DROP COLUMN workspace_id;
