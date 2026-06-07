ALTER TABLE workspace_invitation ADD COLUMN invite_code VARCHAR(8);

-- Allow looking up a pending invitation by its short code.
CREATE UNIQUE INDEX idx_invitation_code_pending
    ON workspace_invitation(invite_code) WHERE status = 'pending' AND invite_code IS NOT NULL;

-- Make invitee_email optional (invite-code flow doesn't require an email).
ALTER TABLE workspace_invitation ALTER COLUMN invitee_email SET DEFAULT '';
