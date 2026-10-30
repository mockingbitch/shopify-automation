-- name: InsertApp :one
INSERT INTO shopee_apps (id, name, environment, api_host, auth_host, partner_id, partner_key_enc,
  dek_wrapped, kek_id, app_type, redirect_uri, push_callback_url, enabled)
VALUES (@id, @name, @environment, @api_host, @auth_host, @partner_id, @partner_key_enc,
  @dek_wrapped, @kek_id, @app_type, @redirect_uri, sqlc.narg(push_callback_url), @enabled)
RETURNING *;

-- name: GetApp :one
SELECT * FROM shopee_apps WHERE id = @id;

-- name: GetAppForUpdate :one
SELECT * FROM shopee_apps WHERE id = @id FOR UPDATE;

-- name: ListApps :many
SELECT * FROM shopee_apps ORDER BY environment, name;

-- name: ListEnabledApps :many
SELECT * FROM shopee_apps WHERE enabled ORDER BY created_at;

-- name: UpdateApp :one
UPDATE shopee_apps SET
  name              = @name,
  api_host          = @api_host,
  auth_host         = @auth_host,
  partner_key_enc   = @partner_key_enc,
  dek_wrapped       = @dek_wrapped,
  kek_id            = @kek_id,
  app_type          = @app_type,
  redirect_uri      = @redirect_uri,
  push_callback_url = sqlc.narg(push_callback_url),
  enabled           = @enabled
WHERE id = @id
RETURNING *;
