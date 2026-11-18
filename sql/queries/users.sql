-- name: GetUserByEmail :one
SELECT id, email, password_hash, first_name, last_name, role, status,
       email_verified_at, created_at, updated_at
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, password_hash, first_name, last_name, role, status,
       email_verified_at, created_at, updated_at
FROM users
WHERE id = $1;
