-- name: ListCapabilityDefinitions :many
SELECT * FROM capability_definitions ORDER BY key;

-- name: ListCapabilityOverrides :many
SELECT * FROM capability_overrides
WHERE shopee_app_id IS NULL OR shopee_app_id = @shopee_app_id
ORDER BY capability_key;

-- name: ListAllOverrides :many
SELECT * FROM capability_overrides ORDER BY capability_key, created_at;

-- name: UpsertOverride :one
INSERT INTO capability_overrides (id, capability_key, shopee_app_id, region, mode, enabled, reason)
VALUES (@id, @capability_key, sqlc.narg(shopee_app_id), sqlc.narg(region), sqlc.narg(mode), sqlc.narg(enabled), @reason)
ON CONFLICT (capability_key, shopee_app_id, region) DO UPDATE SET mode = EXCLUDED.mode, enabled = EXCLUDED.enabled, reason = EXCLUDED.reason
RETURNING *;

-- name: DeleteOverride :execrows
DELETE FROM capability_overrides WHERE id = @id;
