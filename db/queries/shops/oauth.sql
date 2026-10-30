-- name: InsertOAuthState :exec
INSERT INTO oauth_states (id, org_id, user_id, shopee_app_id, token_hash, shop_connection_id, expires_at, created_at)
VALUES (@id, @org_id, @user_id, @shopee_app_id, @token_hash, sqlc.narg(shop_connection_id), @expires_at, @created_at);

-- name: GetOAuthStateByHash :one
SELECT * FROM oauth_states WHERE org_id = @org_id AND token_hash = @token_hash FOR UPDATE;

-- name: ConsumeOAuthState :exec
UPDATE oauth_states SET consumed_at = @now::timestamptz WHERE org_id = @org_id AND id = @id;

-- name: SetOAuthPending :exec
UPDATE oauth_states SET
  main_account_id = @main_account_id, pending_shop_ids = @pending_shop_ids,
  pending_tokens_enc = @pending_tokens_enc, pending_dek_wrapped = @pending_dek_wrapped,
  pending_kek_id = @pending_kek_id, pending_expires_at = @pending_expires_at
WHERE org_id = @org_id AND id = @id;

-- name: GetOAuthState :one
SELECT * FROM oauth_states WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: FinishOAuthSelection :exec
UPDATE oauth_states SET
  selected_at = @now::timestamptz, pending_tokens_enc = NULL, pending_dek_wrapped = NULL, pending_kek_id = NULL
WHERE org_id = @org_id AND id = @id;
