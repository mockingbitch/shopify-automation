-- Queries in this file run on the app_system pool (BYPASSRLS) and may only
-- read the columns granted to that role. They find tenants; all writes then
-- happen in a tenant-scoped transaction on the app_rw pool.

-- name: SystemFindConnections :many
SELECT id, org_id, shopee_app_id, shopee_shop_id, status FROM shop_connections
WHERE shopee_app_id = @shopee_app_id AND shopee_shop_id = ANY(@shop_ids::bigint[])
  AND status NOT IN ('DISCONNECTED', 'REVOKED')
  AND org_id IN (SELECT id FROM organizations WHERE deleted_at IS NULL);

-- name: SystemDueRefreshes :many
SELECT c.shop_connection_id, c.org_id, c.access_expires_at
FROM shop_credentials c
JOIN shop_connections s ON s.id = c.shop_connection_id
JOIN shopee_apps a ON a.id = s.shopee_app_id
JOIN organizations o ON o.id = s.org_id AND o.deleted_at IS NULL
WHERE s.status = 'ACTIVE' AND a.enabled AND c.access_expires_at < @before
ORDER BY c.access_expires_at
LIMIT @lim;
