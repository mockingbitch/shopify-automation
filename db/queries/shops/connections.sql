-- name: InsertConnection :one
INSERT INTO shop_connections (id, org_id, shopee_app_id, shopee_shop_id, status, connected_by, connected_at)
VALUES (@id, @org_id, @shopee_app_id, @shopee_shop_id, @status, @connected_by, @connected_at)
RETURNING *;

-- name: GetConnectionByShop :one
SELECT * FROM shop_connections
WHERE org_id = @org_id AND shopee_app_id = @shopee_app_id AND shopee_shop_id = @shopee_shop_id
FOR UPDATE;

-- name: GetConnection :one
SELECT * FROM shop_connections WHERE org_id = @org_id AND id = @id;

-- name: LockConnection :one
SELECT * FROM shop_connections WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: ListConnections :many
SELECT * FROM shop_connections
WHERE org_id = @org_id
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(region)::text IS NULL OR region = sqlc.narg(region))
  AND (sqlc.narg(q)::text IS NULL OR shop_name ILIKE '%' || sqlc.narg(q) || '%' OR shopee_shop_id::text = sqlc.narg(q))
ORDER BY created_at DESC, id DESC;

-- name: ReactivateConnection :one
UPDATE shop_connections SET
  status = 'ACTIVE', connected_by = @connected_by, connected_at = @connected_at,
  disconnected_at = NULL, last_sync_error = NULL
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: SetConnectionStatus :one
UPDATE shop_connections SET
  status = @status,
  disconnected_at = CASE WHEN @status IN ('DISCONNECTED', 'REVOKED') THEN @now::timestamptz ELSE disconnected_at END
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: UpdateConnectionSynced :one
UPDATE shop_connections SET
  region = @region, shop_name = @shop_name,
  authorization_expires_at = sqlc.narg(authorization_expires_at),
  last_synced_at = @synced_at, last_sync_error = NULL
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: UpdateConnectionSyncFailed :exec
UPDATE shop_connections SET last_sync_error = @error
WHERE org_id = @org_id AND id = @id;

-- name: SetAuthorizationExpiry :exec
UPDATE shop_connections SET authorization_expires_at = @expires_at
WHERE org_id = @org_id AND id = @id;
