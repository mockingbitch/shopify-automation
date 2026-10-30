-- name: InsertEmail :exec
INSERT INTO email_outbox (id, kek_id, dek_wrapped, message_enc, created_at, next_attempt_at)
VALUES (@id, @kek_id, @dek_wrapped, @message_enc, @created_at, @created_at);

-- name: LockEmail :one
SELECT * FROM email_outbox WHERE id = @id AND failed_at IS NULL FOR UPDATE SKIP LOCKED;

-- name: DeleteEmail :exec
DELETE FROM email_outbox WHERE id = @id;

-- name: EmailFailed :exec
UPDATE email_outbox SET attempts = attempts + 1, last_error = @last_error, next_attempt_at = @next_attempt_at,
  failed_at = CASE WHEN attempts + 1 >= @max_attempts::int THEN @now::timestamptz ELSE NULL END
WHERE id = @id;

-- name: SystemDueEmails :many
-- Runs on the app_system pool: emails that should have been sent by now.
SELECT id, attempts FROM email_outbox WHERE failed_at IS NULL AND next_attempt_at < @before ORDER BY next_attempt_at LIMIT @lim;
