DROP INDEX IF EXISTS idx_invitation_code_pending;
ALTER TABLE workspace_invitation DROP COLUMN IF EXISTS invite_code;
ALTER TABLE workspace_invitation ALTER COLUMN invitee_email DROP DEFAULT;
