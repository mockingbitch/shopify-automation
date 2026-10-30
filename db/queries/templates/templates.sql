-- name: InsertTemplate :one
INSERT INTO shop_templates (id, org_id, name, description, cloned_from_version_id, created_by)
VALUES (@id, @org_id, @name, @description, sqlc.narg(cloned_from_version_id), @created_by)
RETURNING *;

-- name: GetTemplate :one
SELECT * FROM shop_templates WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL;

-- name: LockTemplate :one
SELECT * FROM shop_templates WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL FOR UPDATE;

-- name: ListTemplates :many
SELECT sqlc.embed(t), d.id AS draft_version_id, COALESCE(lv.version_no, 0)::int AS latest_version_no
FROM shop_templates t
LEFT JOIN shop_template_versions d ON d.template_id = t.id AND d.status = 'DRAFT'
LEFT JOIN LATERAL (
  SELECT v.version_no FROM shop_template_versions v WHERE v.template_id = t.id ORDER BY v.version_no DESC LIMIT 1
) lv ON true
WHERE t.org_id = @org_id AND t.deleted_at IS NULL
ORDER BY lower(t.name), t.id;

-- name: UpdateTemplate :one
UPDATE shop_templates SET name = @name, description = @description
WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SetPublishedVersion :exec
UPDATE shop_templates SET current_published_version_id = @version_id
WHERE org_id = @org_id AND id = @id;

-- name: SoftDeleteTemplate :exec
UPDATE shop_templates SET deleted_at = @now::timestamptz
WHERE org_id = @org_id AND id = @id AND deleted_at IS NULL;

-- name: TouchTemplate :exec
UPDATE shop_templates SET updated_at = now() WHERE org_id = @org_id AND id = @id;
