-- name: CreateInvitation :one
INSERT INTO organization_invitations (id, org_id, email, role_key, token_hash, invited_by, expires_at)
VALUES (@id, @org_id, @email, @role_key, @token_hash, @invited_by, @expires_at)
RETURNING *;

-- name: ListOpenInvitations :many
SELECT * FROM organization_invitations
WHERE org_id = @org_id AND accepted_at IS NULL AND revoked_at IS NULL
ORDER BY created_at DESC, id;

-- name: GetInvitationByTokenHashForUpdate :one
SELECT * FROM organization_invitations
WHERE org_id = @org_id AND token_hash = @token_hash
FOR UPDATE;

-- name: MarkInvitationAccepted :exec
UPDATE organization_invitations SET accepted_at = @now::timestamptz, accepted_by = @accepted_by::uuid
WHERE org_id = @org_id AND id = @id;

-- name: RevokeInvitation :execrows
UPDATE organization_invitations SET revoked_at = now()
WHERE org_id = @org_id AND id = @id AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: ExpireStaleInvitation :exec
UPDATE organization_invitations SET revoked_at = now()
WHERE org_id = @org_id AND email = @email AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at <= @now;
