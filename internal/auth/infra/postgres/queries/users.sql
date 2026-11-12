-- name: CreateUser :one
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: GetUser :one
SELECT * FROM users
WHERE tenant_id = $1 AND id = $2;

-- name: ListUsers :many
SELECT * FROM users
WHERE tenant_id = $1
ORDER BY created_at;

-- name: MarkEmailVerified :one
UPDATE users SET email_verified_at = now()
WHERE id = $1
RETURNING *;
