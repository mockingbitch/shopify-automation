-- name: UpsertCredentials :exec
INSERT INTO shop_credentials (shop_connection_id, org_id, access_token_enc, refresh_token_enc, dek_wrapped, kek_id,
  access_expires_at, refresh_expires_at, last_refreshed_at)
VALUES (@shop_connection_id, @org_id, @access_token_enc, @refresh_token_enc, @dek_wrapped, @kek_id,
  @access_expires_at, @refresh_expires_at, @now::timestamptz)
ON CONFLICT (shop_connection_id) DO UPDATE SET
  access_token_enc = EXCLUDED.access_token_enc,
  refresh_token_enc = EXCLUDED.refresh_token_enc,
  dek_wrapped = EXCLUDED.dek_wrapped,
  kek_id = EXCLUDED.kek_id,
  access_expires_at = EXCLUDED.access_expires_at,
  refresh_expires_at = EXCLUDED.refresh_expires_at,
  last_refreshed_at = EXCLUDED.last_refreshed_at,
  version = shop_credentials.version + 1,
  consecutive_failures = 0,
  last_error_code = NULL;

-- name: GetCredentials :one
SELECT * FROM shop_credentials WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id;

-- name: GetCredentialsForUpdate :one
SELECT * FROM shop_credentials WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id FOR UPDATE;

-- name: RecordCredentialFailure :exec
UPDATE shop_credentials SET consecutive_failures = consecutive_failures + 1, last_error_code = @code
WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id;

-- name: DeleteCredentials :exec
DELETE FROM shop_credentials WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id;
