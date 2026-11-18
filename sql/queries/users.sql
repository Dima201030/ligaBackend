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

-- name: CreateUser :one
INSERT INTO users (
    email, password_hash, first_name, last_name, role, status
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: CreateUserVerified :one
INSERT INTO users (
    email, password_hash, first_name, last_name, role, status, email_verified_at
) VALUES (
    $1, $2, $3, $4, $5, $6, now()
)
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: MarkEmailVerified :one
UPDATE users
SET email_verified_at = now(), updated_at = now()
WHERE id = $1 AND email_verified_at IS NULL
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: ApproveUser :one
UPDATE users
SET first_name = $2,
    last_name = $3,
    status = 'approved',
    updated_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: RejectUser :one
UPDATE users
SET status = 'rejected', updated_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: ListApplications :many
SELECT
    u.id,
    u.email,
    u.first_name,
    u.last_name,
    u.role,
    u.status,
    u.email_verified_at,
    u.created_at,
    u.updated_at,
    p.phone,
    p.level
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.role = 'player'
ORDER BY
    CASE u.status WHEN 'pending' THEN 0 ELSE 1 END,
    u.created_at DESC;
