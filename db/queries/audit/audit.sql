-- name: InsertAuditLog :exec
INSERT INTO audit_logs (id, org_id, actor_user_id, actor_type, action, target_type, target_id,
                        outcome, metadata, ip, user_agent, request_id, created_at)
VALUES (@id, @org_id, @actor_user_id, @actor_type, @action, @target_type, @target_id,
        @outcome, @metadata, @ip, @user_agent, @request_id, @created_at);

-- name: ListAuditLogs :many
-- Keyset pagination on (created_at, id), newest first. RLS limits rows to the tenant.
SELECT * FROM audit_logs
WHERE org_id = @org_id
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR actor_user_id = sqlc.narg(actor_user_id))
  AND (sqlc.narg(action)::text IS NULL OR action = sqlc.narg(action))
  AND (sqlc.narg(target_type)::text IS NULL OR target_type = sqlc.narg(target_type))
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR created_at >= sqlc.narg(from_time))
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR created_at < sqlc.narg(to_time))
  AND (sqlc.narg(cursor_created_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;
