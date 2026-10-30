-- name: OrgCounts :one
SELECT
  (SELECT count(*) FROM shop_connections s WHERE s.org_id = @org_id AND s.status <> 'DISCONNECTED')::bigint AS connected_shops,
  (SELECT count(*) FROM shop_connections s WHERE s.org_id = @org_id AND s.status = 'ACTIVE')::bigint AS active_shops,
  (SELECT count(*) FROM shop_connections s WHERE s.org_id = @org_id AND s.status IN ('RECONNECT_REQUIRED', 'REVOKED', 'EXPIRED', 'DISCONNECTED'))::bigint AS disconnected_shops,
  (SELECT count(*) FROM shop_connections s WHERE s.org_id = @org_id AND s.status IN ('RECONNECT_REQUIRED', 'REVOKED', 'EXPIRED'))::bigint AS reconnect_required,
  (SELECT count(*) FROM workflow_runs r WHERE r.org_id = @org_id AND r.status IN ('PENDING', 'RUNNING'))::bigint AS running_workflows,
  (SELECT count(*) FROM workflow_runs r WHERE r.org_id = @org_id AND r.status IN ('WAITING_APPROVAL', 'WAITING_USER'))::bigint AS waiting_workflows,
  (SELECT count(*) FROM workflow_runs r WHERE r.org_id = @org_id AND r.status IN ('FAILED', 'PARTIALLY_COMPLETED') AND r.created_at > @since)::bigint AS failed_workflows,
  (SELECT count(*) FROM workflow_runs r WHERE r.org_id = @org_id AND r.status = 'COMPLETED' AND r.definition_key = 'SHOP_SETUP')::bigint AS completed_setups,
  (SELECT count(*) FROM workflow_approvals a WHERE a.org_id = @org_id AND a.status = 'PENDING')::bigint AS pending_approvals,
  (SELECT count(*) FROM browser_tasks b WHERE b.org_id = @org_id AND b.status = 'QUEUED')::bigint AS queued_browser_tasks,
  (SELECT count(*) FROM products p WHERE p.org_id = @org_id AND p.deleted_at IS NULL)::bigint AS products,
  (SELECT count(DISTINCT m.product_id) FROM product_mappings m WHERE m.org_id = @org_id AND m.publish_state = 'VERIFIED')::bigint AS published_products;

-- name: ShopCounts :one
SELECT
  (SELECT count(*) FROM product_mappings m WHERE m.org_id = @org_id AND m.shop_connection_id = @shop_id AND m.publish_state = 'VERIFIED')::bigint AS published_products,
  (SELECT count(*) FROM product_mappings m WHERE m.org_id = @org_id AND m.shop_connection_id = @shop_id AND m.publish_state IN ('FAILED', 'UNKNOWN'))::bigint AS products_needing_attention;

-- name: ShopRuns :many
SELECT id, definition_key, status, input, created_at, finished_at FROM workflow_runs
WHERE org_id = @org_id AND shop_connection_id = @shop_id
ORDER BY created_at DESC LIMIT 20;

-- name: LastSetup :one
SELECT id, status, input, created_at, finished_at FROM workflow_runs
WHERE org_id = @org_id AND shop_connection_id = @shop_id AND definition_key = 'SHOP_SETUP'
ORDER BY created_at DESC LIMIT 1;

-- name: ShopExists :one
SELECT EXISTS (SELECT 1 FROM shop_connections WHERE org_id = @org_id AND id = @id);
