-- name: InsertApproval :one
INSERT INTO workflow_approvals (id, org_id, run_id, step_id, status, diff_snapshot)
VALUES (@id, @org_id, @run_id, @step_id, 'PENDING', @diff_snapshot)
RETURNING *;

-- name: GetApproval :one
SELECT * FROM workflow_approvals WHERE org_id = @org_id AND id = @id;

-- name: LockApproval :one
SELECT * FROM workflow_approvals WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: LatestApprovalForStep :one
SELECT * FROM workflow_approvals WHERE org_id = @org_id AND step_id = @step_id ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: ListApprovals :many
SELECT a.*, r.shop_connection_id, r.created_by AS run_created_by FROM workflow_approvals a
JOIN workflow_runs r ON r.id = a.run_id
WHERE a.org_id = @org_id AND (sqlc.narg(status)::text IS NULL OR a.status = sqlc.narg(status))
ORDER BY a.created_at DESC, a.id DESC
LIMIT 200;

-- name: DecideApproval :one
UPDATE workflow_approvals SET status = @status, selected_changes = sqlc.narg(selected_changes), decided_by = @decided_by,
  comment = sqlc.narg(comment), decided_at = @now::timestamptz
WHERE org_id = @org_id AND id = @id AND status = 'PENDING'
RETURNING *;

-- name: ExpirePendingApprovals :exec
UPDATE workflow_approvals SET status = 'EXPIRED', decided_at = @now::timestamptz
WHERE org_id = @org_id AND run_id = @run_id AND status = 'PENDING';
