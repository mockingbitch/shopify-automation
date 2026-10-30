-- name: InsertBatch :one
INSERT INTO product_import_batches (id, org_id, source, filename, file_storage_key, file_sha256, status, template_id, template_version_id, created_by)
VALUES (@id, @org_id, @source, @filename, @file_storage_key, @file_sha256, 'UPLOADED', sqlc.narg(template_id), sqlc.narg(template_version_id), @created_by)
RETURNING *;

-- name: GetBatch :one
SELECT * FROM product_import_batches WHERE org_id = @org_id AND id = @id;

-- name: LockBatch :one
SELECT * FROM product_import_batches WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: ListBatches :many
SELECT * FROM product_import_batches WHERE org_id = @org_id ORDER BY created_at DESC, id DESC LIMIT 100;

-- name: SetBatchStatus :exec
UPDATE product_import_batches SET status = @status, error = sqlc.narg(error) WHERE org_id = @org_id AND id = @id;

-- name: SetBatchReport :exec
UPDATE product_import_batches SET report_storage_key = @report_storage_key WHERE org_id = @org_id AND id = @id;

-- name: RefreshBatchCounts :one
UPDATE product_import_batches b SET
  total_rows = c.total, valid_rows = c.valid, invalid_rows = c.invalid, imported_rows = c.imported, failed_rows = c.failed
FROM (SELECT count(*)::int AS total,
             count(*) FILTER (WHERE status = 'VALID')::int AS valid,
             count(*) FILTER (WHERE status = 'INVALID')::int AS invalid,
             count(*) FILTER (WHERE status = 'IMPORTED')::int AS imported,
             count(*) FILTER (WHERE status = 'FAILED')::int AS failed
      FROM product_import_rows WHERE org_id = @org_id AND batch_id = @id) c
WHERE b.org_id = @org_id AND b.id = @id
RETURNING b.*;

-- COPY is not allowed on tables with row-level security, so rows are
-- inserted in batches with unnest.
-- name: InsertRows :exec
INSERT INTO product_import_rows (id, org_id, batch_id, row_number, raw, status, errors)
SELECT unnest(@ids::uuid[]), @org_id, @batch_id, unnest(@row_numbers::int[]), unnest(@raws::text[])::jsonb, 'PENDING', '[]'::jsonb;

-- name: DeleteRows :exec
DELETE FROM product_import_rows WHERE org_id = @org_id AND batch_id = @batch_id;

-- name: ListRows :many
SELECT * FROM product_import_rows
WHERE org_id = @org_id AND batch_id = @batch_id
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND row_number > @after_row
ORDER BY row_number
LIMIT @lim;

-- name: AllRows :many
SELECT * FROM product_import_rows WHERE org_id = @org_id AND batch_id = @batch_id ORDER BY row_number;

-- name: LockRow :one
SELECT * FROM product_import_rows WHERE org_id = @org_id AND batch_id = @batch_id AND id = @id FOR UPDATE;

-- name: UpdateRow :exec
UPDATE product_import_rows SET raw = @raw, status = @status, errors = @errors, product_id = sqlc.narg(product_id)
WHERE org_id = @org_id AND id = @id;
