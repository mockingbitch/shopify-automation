-- name: UpsertMapping :one
INSERT INTO shop_template_mappings (id, org_id, template_id, shop_connection_id, overrides, updated_by)
VALUES (@id, @org_id, @template_id, @shop_connection_id, @overrides, @updated_by)
ON CONFLICT (template_id, shop_connection_id) DO UPDATE SET
  overrides = EXCLUDED.overrides, updated_by = EXCLUDED.updated_by
RETURNING *;

-- name: GetMapping :one
SELECT * FROM shop_template_mappings WHERE org_id = @org_id AND template_id = @template_id AND shop_connection_id = @shop_connection_id;

-- name: ListMappings :many
SELECT * FROM shop_template_mappings WHERE org_id = @org_id AND template_id = @template_id ORDER BY created_at;

-- name: ShopExists :one
SELECT EXISTS (SELECT 1 FROM shop_connections WHERE org_id = @org_id AND id = @id);
