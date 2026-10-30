-- name: CreateOrganization :one
INSERT INTO organizations (id, name, slug, created_by)
VALUES (@id, @name, @slug, @created_by)
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = @id AND deleted_at IS NULL;

-- name: ListOrganizationsForUser :many
SELECT sqlc.embed(o), m.role_key
FROM organizations o
JOIN organization_members m ON m.org_id = o.id
WHERE m.user_id = @user_id AND o.deleted_at IS NULL
ORDER BY o.name, o.id;

-- name: UpdateOrganization :one
UPDATE organizations
SET name = COALESCE(sqlc.narg(name), name),
    settings = COALESCE(sqlc.narg(settings), settings)
WHERE id = @id AND deleted_at IS NULL AND updated_at = @expected_updated_at
RETURNING *;

-- name: SoftDeleteOrganization :execrows
UPDATE organizations SET deleted_at = now() WHERE id = @id AND deleted_at IS NULL;
