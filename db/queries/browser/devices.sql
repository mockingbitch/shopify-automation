-- name: InsertPairingCode :exec
INSERT INTO extension_pairing_codes (id, org_id, user_id, code_hash, expires_at)
VALUES (@id, @org_id, @user_id, @code_hash, @expires_at);

-- name: LockPairingCode :one
SELECT * FROM extension_pairing_codes WHERE org_id = @org_id AND id = @id FOR UPDATE;

-- name: UsePairingCode :exec
UPDATE extension_pairing_codes SET used_at = @used_at WHERE org_id = @org_id AND id = @id;

-- name: InsertDevice :one
INSERT INTO extension_devices (id, org_id, user_id, name, extension_version, token_hash, expires_at, last_used_at)
VALUES (@id, @org_id, @user_id, @name, @extension_version, @token_hash, @expires_at, @last_used_at)
RETURNING *;

-- name: GetDevice :one
SELECT * FROM extension_devices WHERE org_id = @org_id AND id = @id;

-- name: TouchDevice :exec
UPDATE extension_devices SET last_used_at = @last_used_at, expires_at = @expires_at, extension_version = @extension_version
WHERE org_id = @org_id AND id = @id;

-- name: RotateDeviceToken :exec
UPDATE extension_devices SET token_hash = @token_hash, expires_at = @expires_at, last_used_at = @last_used_at
WHERE org_id = @org_id AND id = @id AND revoked_at IS NULL;

-- name: RevokeDevice :execrows
UPDATE extension_devices SET revoked_at = @revoked_at, revoked_by = @revoked_by
WHERE org_id = @org_id AND id = @id AND revoked_at IS NULL;

-- name: ListDevices :many
SELECT d.*, u.email AS user_email
FROM extension_devices d JOIN users u ON u.id = d.user_id
WHERE d.org_id = @org_id AND d.revoked_at IS NULL AND d.expires_at > @now
ORDER BY d.created_at DESC;

-- name: CountActiveDevices :one
SELECT count(*) FROM extension_devices WHERE org_id = @org_id AND revoked_at IS NULL AND expires_at > @now;

-- name: MemberRole :one
SELECT role_key FROM organization_members WHERE org_id = @org_id AND user_id = @user_id;

-- name: DeviceContext :one
SELECT o.name AS org_name, u.email FROM organizations o, users u WHERE o.id = @org_id AND u.id = @user_id;
