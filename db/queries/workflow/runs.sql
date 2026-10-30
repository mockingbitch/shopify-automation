-- name: InsertRun :one
INSERT INTO workflow_runs (id, org_id, definition_key, definition_version, shop_connection_id, batch_id, status,
  input, idempotency_key, request_hash, created_by)
VALUES (@id, @org_id, @definition_key, @definition_version, @shop_connection_id, sqlc.narg(batch_id), 'PENDING',
  @input, sqlc.narg(idempotency_key), sqlc.narg(request_hash), sqlc.narg(created_by))
RETURNING *;

-- name: GetRun :one
SELECT * FROM workflow_runs WHERE org_id = @org_id AND id = @id;

-- name: LockRun :one
SELECT * FROM workflow_runs WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: ListRuns :many
SELECT * FROM workflow_runs
WHERE org_id = @org_id
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(shop_connection_id)::uuid IS NULL OR shop_connection_id = sqlc.narg(shop_connection_id))
  AND (sqlc.narg(batch_id)::uuid IS NULL OR batch_id = sqlc.narg(batch_id))
  AND (sqlc.narg(before)::timestamptz IS NULL OR created_at < sqlc.narg(before))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: RunsByIdempotencyKey :many
SELECT * FROM workflow_runs WHERE org_id = @org_id AND idempotency_key = @idempotency_key ORDER BY created_at, id;

-- name: SetRunStatus :one
UPDATE workflow_runs SET
  status = @status,
  started_at = COALESCE(started_at, sqlc.narg(started_at)),
  finished_at = sqlc.narg(finished_at)
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: SetRunOutput :exec
UPDATE workflow_runs SET output = @output WHERE org_id = @org_id AND id = @id;

-- name: RequestCancel :exec
UPDATE workflow_runs SET cancel_requested_at = @now::timestamptz, cancel_reason = sqlc.narg(reason)
WHERE org_id = @org_id AND id = @id AND cancel_requested_at IS NULL;

-- name: ActiveRunsForTemplate :one
SELECT count(*) FROM workflow_runs
WHERE org_id = @org_id AND status IN ('PENDING', 'RUNNING', 'WAITING_APPROVAL', 'WAITING_USER')
  AND input->>'template_id' = @template_id::text;

-- name: AdvisoryLock :exec
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));
