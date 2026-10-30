-- name: CreateMember :one
INSERT INTO organization_members (id, org_id, user_id, role_key, invited_by)
VALUES (@id, @org_id, @user_id, @role_key, @invited_by)
RETURNING *;

-- name: GetMembership :one
SELECT * FROM organization_members WHERE org_id = @org_id AND user_id = @user_id;

-- name: GetMemberForUpdate :one
SELECT * FROM organization_members WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: ListMembers :many
SELECT m.id, m.org_id, m.user_id, m.role_key, m.created_at, u.email, u.display_name
FROM organization_members m
JOIN users u ON u.id = m.user_id
WHERE m.org_id = @org_id
ORDER BY m.created_at, m.id;

-- name: UpdateMemberRole :one
UPDATE organization_members SET role_key = @role_key
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: DeleteMember :execrows
DELETE FROM organization_members WHERE org_id = @org_id AND id = @id;

-- name: IsMemberByEmail :one
SELECT EXISTS (
  SELECT 1 FROM organization_members m JOIN users u ON u.id = m.user_id
  WHERE m.org_id = @org_id AND u.email = @email AND u.deleted_at IS NULL
);

-- name: GetMemberByUserForUpdate :one
SELECT * FROM organization_members WHERE org_id = @org_id AND user_id = @user_id FOR UPDATE;
