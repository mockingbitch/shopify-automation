-- name: ListCapabilityDefinitions :many
SELECT * FROM capability_definitions ORDER BY key;

-- name: ListCapabilityOverrides :many
SELECT * FROM capability_overrides
WHERE shopee_app_id IS NULL OR shopee_app_id = @shopee_app_id
ORDER BY capability_key;
