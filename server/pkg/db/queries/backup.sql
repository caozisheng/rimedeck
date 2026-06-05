-- Backup & Restore queries

-- name: GetAgentByWorkspaceAndName :one
SELECT * FROM agent
WHERE workspace_id = $1 AND name = $2;

-- name: GetSquadByWorkspaceAndName :one
SELECT * FROM squad
WHERE workspace_id = $1 AND name = $2;

-- name: ListSkillFilesByWorkspace :many
SELECT sf.* FROM skill_file sf
JOIN skill s ON s.id = sf.skill_id
WHERE s.workspace_id = $1
ORDER BY sf.skill_id, sf.path ASC;

-- name: GetMemberByEmailAndWorkspace :one
SELECT m.* FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE u.email = $1 AND m.workspace_id = $2;
