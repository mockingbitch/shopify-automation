-- name: InsertProduct :one
INSERT INTO products (id, org_id, sku, name, description, shopee_category_id, category_label, brand_name, condition,
  attributes, price, currency, stock, weight_kg, package_cm, days_to_ship, tiers, source, import_batch_id, created_by)
VALUES (@id, @org_id, @sku, @name, @description, sqlc.narg(shopee_category_id), sqlc.narg(category_label), sqlc.narg(brand_name),
  sqlc.narg(condition), @attributes, @price, @currency, @stock, sqlc.narg(weight_kg), sqlc.narg(package_cm), sqlc.narg(days_to_ship),
  @tiers, @source, sqlc.narg(import_batch_id), sqlc.narg(created_by))
RETURNING *;

-- name: UpdateProduct :one
UPDATE products SET sku = @sku, name = @name, description = @description, shopee_category_id = sqlc.narg(shopee_category_id),
  category_label = sqlc.narg(category_label), brand_name = sqlc.narg(brand_name), condition = sqlc.narg(condition),
  attributes = @attributes, price = @price, currency = @currency, stock = @stock, weight_kg = sqlc.narg(weight_kg),
  package_cm = sqlc.narg(package_cm), days_to_ship = sqlc.narg(days_to_ship), tiers = @tiers,
  import_batch_id = COALESCE(sqlc.narg(import_batch_id), import_batch_id)
WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL
RETURNING *;

-- name: GetProduct :one
SELECT * FROM products WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL;

-- name: LockProduct :one
SELECT * FROM products WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL FOR UPDATE;

-- name: GetProductBySKU :one
SELECT * FROM products WHERE org_id = @org_id AND sku = @sku AND deleted_at IS NULL FOR UPDATE;

-- name: ListProducts :many
SELECT * FROM products
WHERE org_id = @org_id AND deleted_at IS NULL
  AND (sqlc.narg(q)::text IS NULL OR name ILIKE '%' || sqlc.narg(q) || '%' OR sku::text ILIKE '%' || sqlc.narg(q) || '%')
  AND (sqlc.narg(batch_id)::uuid IS NULL OR import_batch_id = sqlc.narg(batch_id))
  AND (sqlc.narg(before)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(before)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: ProductsByIDs :many
SELECT * FROM products WHERE org_id = @org_id AND id = ANY(@ids::uuid[]) AND deleted_at IS NULL;

-- name: ProductIDsByBatch :many
SELECT id FROM products WHERE org_id = @org_id AND import_batch_id = @batch_id AND deleted_at IS NULL ORDER BY created_at, id;

-- name: SoftDeleteProduct :exec
UPDATE products SET deleted_at = @now::timestamptz WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteVariations :exec
DELETE FROM product_variations WHERE org_id = @org_id AND product_id = @product_id;

-- name: InsertVariation :exec
INSERT INTO product_variations (id, org_id, product_id, tier_index, sku, price, stock)
VALUES (@id, @org_id, @product_id, @tier_index, sqlc.narg(sku), @price, @stock);

-- name: ListVariations :many
SELECT * FROM product_variations WHERE org_id = @org_id AND product_id = ANY(@product_ids::uuid[]) ORDER BY product_id, tier_index;

-- name: InsertImage :one
INSERT INTO product_images (id, org_id, product_id, position, storage_key, content_type, sha256, width, height, source_url)
VALUES (@id, @org_id, @product_id, @position, @storage_key, @content_type, @sha256, @width, @height, sqlc.narg(source_url))
RETURNING *;

-- name: ListImages :many
SELECT * FROM product_images WHERE org_id = @org_id AND product_id = ANY(@product_ids::uuid[]) ORDER BY product_id, position, created_at;

-- name: DeleteImage :execrows
DELETE FROM product_images WHERE org_id = @org_id AND product_id = @product_id AND id = @id;

-- name: DeleteImages :exec
DELETE FROM product_images WHERE org_id = @org_id AND product_id = @product_id;

-- name: CountImages :one
SELECT count(*)::int FROM product_images WHERE org_id = @org_id AND product_id = @product_id;
