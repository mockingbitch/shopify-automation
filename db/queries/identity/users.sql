-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, display_name)
VALUES (@id, @email, @password_hash, @display_name)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = @email AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id AND deleted_at IS NULL;

-- name: MarkEmailVerified :exec
UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = @id;

-- name: UpdatePasswordHash :exec
UPDATE users SET password_hash = @password_hash WHERE id = @id;

-- name: UpdateDisplayName :one
UPDATE users SET display_name = @display_name WHERE id = @id AND deleted_at IS NULL
RETURNING *;
