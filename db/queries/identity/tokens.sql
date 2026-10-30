-- name: CreateUserToken :exec
INSERT INTO user_tokens (id, user_id, purpose, token_hash, expires_at)
VALUES (@id, @user_id, @purpose, @token_hash, @expires_at);

-- name: ConsumeUserToken :one
UPDATE user_tokens
SET used_at = @now::timestamptz
WHERE token_hash = @token_hash
  AND purpose = @purpose
  AND used_at IS NULL
  AND expires_at > @now
RETURNING user_id;

-- name: InvalidateUserTokens :exec
UPDATE user_tokens SET used_at = now()
WHERE user_id = @user_id AND purpose = @purpose AND used_at IS NULL;
