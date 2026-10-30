-- name: InsertAuthorization :exec
INSERT INTO shop_authorizations (id, org_id, shop_connection_id, event, source, auth_time, expire_time, detail, created_at)
VALUES (@id, @org_id, @shop_connection_id, @event, @source, sqlc.narg(auth_time), sqlc.narg(expire_time), @detail, @created_at);

-- name: ListAuthorizations :many
SELECT * FROM shop_authorizations
WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: UpsertProfile :exec
INSERT INTO shop_profiles (shop_connection_id, org_id, shop_name, region, status, shop_logo_url, description,
  is_cb, is_sip, merchant_id, raw, fetched_at)
VALUES (@shop_connection_id, @org_id, @shop_name, @region, @status, @shop_logo_url, @description,
  @is_cb, @is_sip, sqlc.narg(merchant_id), @raw, @fetched_at)
ON CONFLICT (shop_connection_id) DO UPDATE SET
  shop_name = EXCLUDED.shop_name, region = EXCLUDED.region, status = EXCLUDED.status,
  shop_logo_url = EXCLUDED.shop_logo_url, description = EXCLUDED.description,
  is_cb = EXCLUDED.is_cb, is_sip = EXCLUDED.is_sip, merchant_id = EXCLUDED.merchant_id,
  raw = EXCLUDED.raw, fetched_at = EXCLUDED.fetched_at;

-- name: GetProfile :one
SELECT * FROM shop_profiles WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id;

-- name: DeleteProfile :exec
DELETE FROM shop_profiles WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id;
