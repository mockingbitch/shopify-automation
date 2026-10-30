-- name: InsertStep :exec
INSERT INTO workflow_steps (id, org_id, run_id, step_key, item_key, position, status, blocking, mutating,
  capability_key, max_attempts, next_attempt_at)
VALUES (@id, @org_id, @run_id, @step_key, '', @position, 'PENDING', @blocking, @mutating,
  sqlc.narg(capability_key), @max_attempts, @next_attempt_at);

-- name: ListSteps :many
SELECT * FROM workflow_steps WHERE org_id = @org_id AND run_id = @run_id ORDER BY position, item_key;

-- name: GetStep :one
SELECT * FROM workflow_steps WHERE org_id = @org_id AND id = @id;

-- name: LockStep :one
SELECT * FROM workflow_steps WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: PreviousStepsDone :one
SELECT NOT EXISTS (
  SELECT 1 FROM workflow_steps p
  WHERE p.run_id = @run_id AND p.position < @position
    AND NOT (p.status IN ('SUCCEEDED', 'SKIPPED') OR (p.status = 'FAILED' AND NOT p.blocking))
)::bool;

-- name: OtherMutatingRunning :one
SELECT EXISTS (
  SELECT 1 FROM workflow_steps s JOIN workflow_runs r ON r.id = s.run_id
  WHERE r.org_id = @org_id AND r.shop_connection_id = @shop_connection_id AND s.id <> @step_id
    AND s.mutating AND s.status = 'RUNNING' AND s.lease_expires_at > @now::timestamptz
)::bool;

-- name: ClaimStep :one
UPDATE workflow_steps SET status = 'RUNNING', lease_token = @lease_token, lease_expires_at = @lease_expires_at,
  attempt_count = attempt_count + 1, started_at = COALESCE(started_at, @now::timestamptz), execution_mode = sqlc.narg(execution_mode)
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: DelayStep :exec
UPDATE workflow_steps SET next_attempt_at = @next_attempt_at WHERE org_id = @org_id AND id = @id AND status = 'PENDING';

-- name: FinishStep :one
UPDATE workflow_steps SET status = @status, lease_token = NULL, lease_expires_at = NULL,
  next_attempt_at = @next_attempt_at, output = sqlc.narg(output), error = sqlc.narg(error),
  skip_reason = sqlc.narg(skip_reason), finished_at = sqlc.narg(finished_at)
WHERE org_id = @org_id AND id = @id AND status = 'RUNNING' AND lease_token = @lease_token
RETURNING *;

-- name: SetStepStatus :one
UPDATE workflow_steps SET status = @status, lease_token = NULL, lease_expires_at = NULL,
  next_attempt_at = @next_attempt_at, output = COALESCE(sqlc.narg(output), output),
  finished_at = sqlc.narg(finished_at), skip_reason = COALESCE(sqlc.narg(skip_reason), skip_reason)
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: ResetStepForRetry :exec
UPDATE workflow_steps SET status = 'PENDING', attempt_count = 0, next_attempt_at = @now::timestamptz,
  error = NULL, finished_at = NULL
WHERE org_id = @org_id AND id = @id AND status = 'FAILED';

-- name: InsertAttempt :exec
INSERT INTO workflow_step_attempts (id, org_id, step_id, attempt_no, outcome, error, trace_id, started_at, finished_at)
VALUES (@id, @org_id, @step_id, @attempt_no, @outcome, sqlc.narg(error), sqlc.narg(trace_id), @started_at, @finished_at);

-- name: ListAttempts :many
SELECT * FROM workflow_step_attempts WHERE org_id = @org_id AND step_id = @step_id ORDER BY attempt_no, started_at;
