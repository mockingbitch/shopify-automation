-- name: GetCatalog :one
SELECT payload, fetched_at FROM shopee_catalog_cache
WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id AND kind = @kind AND cache_key = @cache_key;

-- name: PutCatalog :exec
INSERT INTO shopee_catalog_cache (org_id, shop_connection_id, kind, cache_key, payload, fetched_at)
VALUES (@org_id, @shop_connection_id, @kind, @cache_key, @payload, @fetched_at)
ON CONFLICT (shop_connection_id, kind, cache_key) DO UPDATE SET payload = EXCLUDED.payload, fetched_at = EXCLUDED.fetched_at;
