-- name: InsertSection :exec
INSERT INTO shop_template_sections (id, org_id, version_id, section_key, content, updated_by)
VALUES (@id, @org_id, @version_id, @section_key, @content, @updated_by);

-- name: ListSections :many
SELECT * FROM shop_template_sections WHERE org_id = @org_id AND version_id = @version_id ORDER BY section_key;

-- name: LockSection :one
SELECT * FROM shop_template_sections WHERE org_id = @org_id AND version_id = @version_id AND section_key = @section_key FOR UPDATE;

-- name: UpdateSection :one
UPDATE shop_template_sections SET content = @content, updated_by = @updated_by
WHERE org_id = @org_id AND version_id = @version_id AND section_key = @section_key
RETURNING *;
