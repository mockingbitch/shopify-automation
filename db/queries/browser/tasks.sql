-- name: InsertTask :one
INSERT INTO browser_tasks (id, org_id, shop_connection_id, run_id, step_id, task_key, capability_key, market, title, input, preview)
VALUES (@id, @org_id, @shop_connection_id, @run_id, @step_id, @task_key, @capability_key, @market, @title, @input, @preview)
ON CONFLICT (step_id, task_key) DO UPDATE SET task_key = EXCLUDED.task_key
RETURNING *;

-- name: GetTask :one
SELECT * FROM browser_tasks WHERE org_id = @org_id AND id = @id;

-- name: LockTask :one
SELECT * FROM browser_tasks WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: TasksForDevice :many
-- Queued tasks of the org, plus tasks this device holds. Expired leases
-- count as queued.
SELECT sqlc.embed(t), s.shopee_shop_id, s.shop_name, st.status AS step_status
FROM browser_tasks t
JOIN shop_connections s ON s.org_id = t.org_id AND s.id = t.shop_connection_id
JOIN workflow_steps st ON st.org_id = t.org_id AND st.id = t.step_id
WHERE t.org_id = @org_id
  AND (t.status = 'QUEUED'
       OR (t.status IN ('RUNNING', 'WAITING_USER') AND (t.device_id = @device_id OR t.lease_expires_at < @now)))
ORDER BY t.created_at
LIMIT 100;

-- name: TasksForRun :many
SELECT * FROM browser_tasks WHERE org_id = @org_id AND run_id = @run_id ORDER BY created_at, task_key;

-- name: TasksForStep :many
SELECT * FROM browser_tasks WHERE org_id = @org_id AND step_id = @step_id ORDER BY task_key;

-- name: ClaimTask :exec
UPDATE browser_tasks SET status = 'RUNNING', device_id = @device_id, lease_token = @lease_token, lease_expires_at = @lease_expires_at,
  recipe_id = @recipe_id, attempt_count = attempt_count + 1, status_message = NULL
WHERE org_id = @org_id AND id = @id;

-- name: HeartbeatTask :execrows
UPDATE browser_tasks SET status = @status, lease_expires_at = @lease_expires_at, status_message = sqlc.narg(status_message)
WHERE org_id = @org_id AND id = @id AND lease_token = @lease_token AND status IN ('RUNNING', 'WAITING_USER');

-- name: FinishTask :execrows
UPDATE browser_tasks SET status = @status, lease_token = NULL, lease_expires_at = NULL, last_error = sqlc.narg(last_error),
  step_log = @step_log, status_message = sqlc.narg(status_message), finished_at = @finished_at
WHERE org_id = @org_id AND id = @id AND lease_token = @lease_token AND status IN ('RUNNING', 'WAITING_USER');

-- name: SetTaskStatus :exec
UPDATE browser_tasks SET status = @status, lease_token = NULL, lease_expires_at = NULL, device_id = NULL,
  status_message = sqlc.narg(status_message), finished_at = sqlc.narg(finished_at)
WHERE org_id = @org_id AND id = @id;

-- name: CancelStepTasks :exec
UPDATE browser_tasks SET status = 'CANCELLED', lease_token = NULL, lease_expires_at = NULL, finished_at = @now,
  status_message = @message
WHERE org_id = @org_id AND step_id = @step_id AND status IN ('QUEUED', 'RUNNING', 'WAITING_USER');

-- name: InsertScreenshot :exec
INSERT INTO browser_task_screenshots (id, org_id, task_id, storage_key) VALUES (@id, @org_id, @task_id, @storage_key);

-- name: CountScreenshots :one
SELECT count(*) FROM browser_task_screenshots WHERE org_id = @org_id AND task_id = @task_id;

-- name: GetScreenshot :one
SELECT * FROM browser_task_screenshots WHERE org_id = @org_id AND task_id = @task_id AND id = @id;

-- name: ScreenshotsForTasks :many
SELECT * FROM browser_task_screenshots WHERE org_id = @org_id AND task_id = ANY(@task_ids::uuid[]) ORDER BY created_at;

-- name: StepState :one
SELECT st.status, st.run_id, r.shop_connection_id FROM workflow_steps st JOIN workflow_runs r ON r.org_id = st.org_id AND r.id = st.run_id
WHERE st.org_id = @org_id AND st.id = @step_id;

-- name: ShopRegion :one
SELECT region, shopee_shop_id, shop_name, status FROM shop_connections WHERE org_id = @org_id AND id = @id;

-- name: CapabilityState :one
-- Global kill switches: the definition and overrides that apply to every app.
SELECT d.enabled AND NOT EXISTS (
  SELECT 1 FROM capability_overrides o
  WHERE o.capability_key = d.key AND o.shopee_app_id IS NULL AND (o.region IS NULL OR o.region = @market::text) AND o.enabled = false
) AS enabled, d.default_mode
FROM capability_definitions d WHERE d.key = @key;

-- name: RunExists :one
SELECT EXISTS (SELECT 1 FROM workflow_runs WHERE org_id = @org_id AND id = @id);
