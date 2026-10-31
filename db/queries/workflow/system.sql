-- Runs on the app_system pool. Finds steps that should be executing but have
-- no live task: runnable PENDING steps whose previous steps are done, and
-- RUNNING steps whose lease expired (crashed worker).

-- name: SystemRunnableSteps :many
SELECT s.id, s.org_id, s.attempt_count, s.next_attempt_at
FROM workflow_steps s
JOIN workflow_runs r ON r.id = s.run_id
JOIN organizations o ON o.id = s.org_id AND o.deleted_at IS NULL
WHERE r.cancel_requested_at IS NULL
  AND r.status IN ('PENDING', 'RUNNING', 'WAITING_USER', 'WAITING_APPROVAL')
  AND (
    (s.status = 'PENDING' AND s.next_attempt_at <= @stale_before
      AND NOT EXISTS (
        SELECT 1 FROM workflow_steps p
        WHERE p.run_id = s.run_id AND p.position < s.position
          AND NOT (p.status IN ('SUCCEEDED', 'SKIPPED') OR (p.status = 'FAILED' AND NOT p.blocking))))
    OR (s.status = 'RUNNING' AND s.lease_expires_at < @now)
  )
ORDER BY s.next_attempt_at
LIMIT @lim;
