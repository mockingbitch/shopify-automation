-- Queries in this file run on the app_system pool (BYPASSRLS) and may only
-- read the columns granted to that role. They find the organization of a
-- pairing code or device token; everything else happens in a tenant scope.

-- name: SystemFindPairingCode :one
SELECT id, org_id FROM extension_pairing_codes WHERE code_hash = @code_hash;

-- name: SystemFindDevice :one
SELECT id, org_id FROM extension_devices WHERE token_hash = @token_hash;
