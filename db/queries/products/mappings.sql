-- name: ListMappings :many
SELECT * FROM product_mappings WHERE org_id = @org_id AND product_id = ANY(@product_ids::uuid[]) ORDER BY created_at;

-- name: GetMapping :one
SELECT * FROM product_mappings WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id AND product_id = @product_id;

-- name: LockMapping :one
SELECT * FROM product_mappings WHERE org_id = @org_id AND shop_connection_id = @shop_connection_id AND product_id = @product_id FOR UPDATE;

-- name: InsertMapping :one
INSERT INTO product_mappings (id, org_id, shop_connection_id, product_id, publish_state, idempotency_key, item_sku, last_run_id)
VALUES (@id, @org_id, @shop_connection_id, @product_id, 'PLANNED', @idempotency_key, @item_sku, sqlc.narg(last_run_id))
ON CONFLICT (shop_connection_id, product_id) DO UPDATE SET last_run_id = EXCLUDED.last_run_id
RETURNING *;

-- name: SetMappingState :one
UPDATE product_mappings SET publish_state = @publish_state, shopee_item_id = COALESCE(sqlc.narg(shopee_item_id), shopee_item_id),
  models = COALESCE(sqlc.narg(models), models), last_error = sqlc.narg(last_error)
WHERE org_id = @org_id AND id = @id
RETURNING *;

-- name: GetImageUpload :one
SELECT * FROM product_image_uploads WHERE org_id = @org_id AND product_image_id = @product_image_id AND shop_connection_id = @shop_connection_id;

-- name: InsertImageUpload :exec
INSERT INTO product_image_uploads (id, org_id, product_image_id, shop_connection_id, shopee_image_id, shopee_image_url)
VALUES (@id, @org_id, @product_image_id, @shop_connection_id, @shopee_image_id, sqlc.narg(shopee_image_url))
ON CONFLICT (product_image_id, shop_connection_id) DO NOTHING;
