-- name: CreateSession :exec
INSERT INTO user_sessions (id, user_id, token_hash, csrf_secret, ip, user_agent, idle_expires_at, absolute_expires_at)
VALUES (@id, @user_id, @token_hash, @csrf_secret, @ip, @user_agent, @idle_expires_at, @absolute_expires_at);

-- name: GetActiveSessionByTokenHash :one
SELECT sqlc.embed(s), sqlc.embed(u),
  EXISTS (SELECT 1 FROM user_mfa m WHERE m.user_id = u.id AND m.confirmed_at IS NOT NULL) AS mfa_enabled
FROM user_sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash
  AND s.revoked_at IS NULL
  AND s.idle_expires_at > @now
  AND s.absolute_expires_at > @now
  AND u.deleted_at IS NULL;

-- name: TouchSession :exec
UPDATE user_sessions
SET last_seen_at = @now,
    idle_expires_at = LEAST(@idle_expires_at, absolute_expires_at)
WHERE id = @id;

-- name: RevokeSession :exec
UPDATE user_sessions SET revoked_at = now() WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE user_sessions SET revoked_at = now() WHERE user_id = @user_id AND revoked_at IS NULL;
