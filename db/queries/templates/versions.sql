-- name: InsertVersion :one
INSERT INTO shop_template_versions (id, org_id, template_id, version_no, status, based_on_id, created_by)
VALUES (@id, @org_id, @template_id,
  COALESCE((SELECT max(version_no) FROM shop_template_versions WHERE template_id = @template_id), 0) + 1,
  'DRAFT', sqlc.narg(based_on_id), @created_by)
RETURNING *;

-- name: GetVersion :one
SELECT * FROM shop_template_versions WHERE org_id = @org_id AND template_id = @template_id AND id = @id;

-- name: LockVersion :one
SELECT * FROM shop_template_versions WHERE org_id = @org_id AND template_id = @template_id AND id = @id FOR UPDATE;

-- name: ListVersions :many
SELECT * FROM shop_template_versions WHERE org_id = @org_id AND template_id = @template_id
ORDER BY version_no DESC;

-- name: PublishVersion :one
UPDATE shop_template_versions SET status = 'PUBLISHED', content_sha256 = @content_sha256,
  published_at = @now::timestamptz, published_by = @published_by
WHERE org_id = @org_id AND id = @id AND status = 'DRAFT'
RETURNING *;

-- name: ArchiveVersion :exec
UPDATE shop_template_versions SET status = 'ARCHIVED'
WHERE org_id = @org_id AND id = @id AND status = 'PUBLISHED';
