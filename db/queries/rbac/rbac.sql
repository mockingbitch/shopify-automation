-- name: ListRolePermissions :many
SELECT role_key, permission_key FROM role_permissions ORDER BY role_key, permission_key;

-- name: ListRoles :many
SELECT key, rank FROM roles ORDER BY rank;
