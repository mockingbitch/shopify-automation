-- name: GetMFA :one
SELECT * FROM user_mfa WHERE user_id = @user_id;

-- name: LockMFA :one
SELECT * FROM user_mfa WHERE user_id = @user_id FOR UPDATE;

-- name: UpsertPendingMFA :exec
-- A new secret replaces an unconfirmed one; a confirmed one is never replaced here.
INSERT INTO user_mfa (user_id, kek_id, dek_wrapped, secret_enc)
VALUES (@user_id, @kek_id, @dek_wrapped, @secret_enc)
ON CONFLICT (user_id) DO UPDATE SET kek_id = EXCLUDED.kek_id, dek_wrapped = EXCLUDED.dek_wrapped, secret_enc = EXCLUDED.secret_enc,
  last_used_step = 0
WHERE user_mfa.confirmed_at IS NULL;

-- name: ConfirmMFA :exec
UPDATE user_mfa SET confirmed_at = @now, last_used_step = @step WHERE user_id = @user_id;

-- name: SetMFAStep :exec
UPDATE user_mfa SET last_used_step = @step WHERE user_id = @user_id;

-- name: DeleteMFA :exec
DELETE FROM user_mfa WHERE user_id = @user_id;

-- name: DeleteRecoveryCodes :exec
DELETE FROM user_recovery_codes WHERE user_id = @user_id;

-- name: InsertRecoveryCode :exec
INSERT INTO user_recovery_codes (id, user_id, code_hash) VALUES (@id, @user_id, @code_hash);

-- name: UseRecoveryCode :execrows
UPDATE user_recovery_codes SET used_at = @now WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL;

-- name: CountRecoveryCodes :one
SELECT count(*) FROM user_recovery_codes WHERE user_id = @user_id AND used_at IS NULL;

-- name: InsertChallenge :exec
INSERT INTO mfa_challenges (id, user_id, token_hash, expires_at) VALUES (@id, @user_id, @token_hash, @expires_at);

-- name: LockChallenge :one
SELECT c.*, u.email, u.display_name, u.is_platform_admin, u.email_verified_at
FROM mfa_challenges c JOIN users u ON u.id = c.user_id
WHERE c.token_hash = @token_hash AND u.deleted_at IS NULL
FOR UPDATE OF c;

-- name: ChallengeAttempt :exec
UPDATE mfa_challenges SET attempts = attempts + 1 WHERE id = @id;

-- name: UseChallenge :exec
UPDATE mfa_challenges SET used_at = @now WHERE id = @id;
