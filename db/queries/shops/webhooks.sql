-- name: InsertWebhookEvent :one
INSERT INTO webhook_events (id, shopee_app_id, push_code, shopee_shop_ids, body_sha256, body, received_at)
VALUES (@id, @shopee_app_id, @push_code, @shopee_shop_ids, @body_sha256, @body, @received_at)
ON CONFLICT (shopee_app_id, body_sha256) DO NOTHING
RETURNING id;

-- name: GetWebhookEventForUpdate :one
SELECT * FROM webhook_events WHERE id = @id FOR UPDATE;

-- name: FinishWebhookEvent :exec
UPDATE webhook_events SET status = @status, attempts = attempts + 1, last_error = sqlc.narg(last_error),
  processed_at = CASE WHEN @status = 'RECEIVED' THEN NULL ELSE @now::timestamptz END
WHERE id = @id;

-- name: ListPendingWebhookEvents :many
SELECT id FROM webhook_events WHERE status = 'RECEIVED' AND received_at < @before ORDER BY received_at LIMIT @lim;
