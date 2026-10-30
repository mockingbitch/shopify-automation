-- name: ListNotifications :many
SELECT * FROM notifications
WHERE org_id = @org_id AND user_id = @user_id AND (NOT @unread_only::bool OR read_at IS NULL)
ORDER BY created_at DESC
LIMIT @lim;

-- name: CountUnread :one
SELECT count(*) FROM notifications WHERE org_id = @org_id AND user_id = @user_id AND read_at IS NULL;

-- name: MarkRead :execrows
UPDATE notifications SET read_at = @now WHERE org_id = @org_id AND user_id = @user_id AND id = @id AND read_at IS NULL;

-- name: MarkAllRead :exec
UPDATE notifications SET read_at = @now WHERE org_id = @org_id AND user_id = @user_id AND read_at IS NULL;
