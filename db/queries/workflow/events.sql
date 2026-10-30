-- name: InsertEvent :exec
INSERT INTO workflow_events (id, org_id, run_id, step_id, type, level, message, data, created_at)
VALUES (@id, @org_id, @run_id, sqlc.narg(step_id), @type, @level, @message, @data, @created_at);

-- name: ListRunEvents :many
SELECT * FROM workflow_events
WHERE org_id = @org_id AND run_id = @run_id
  AND (sqlc.narg(after_at)::timestamptz IS NULL OR (created_at, id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at, id
LIMIT @lim;

-- name: ListOrgEvents :many
SELECT * FROM workflow_events
WHERE org_id = @org_id
  AND (sqlc.narg(level)::text IS NULL OR level = sqlc.narg(level))
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: InsertSnapshot :exec
INSERT INTO shop_config_snapshots (id, org_id, shop_connection_id, kind, workflow_run_id, config, captured_at)
VALUES (@id, @org_id, @shop_connection_id, @kind, sqlc.narg(workflow_run_id), @config, @captured_at);

-- name: RunSnapshot :one
SELECT * FROM shop_config_snapshots WHERE org_id = @org_id AND workflow_run_id = @workflow_run_id AND kind = @kind
ORDER BY captured_at DESC LIMIT 1;
