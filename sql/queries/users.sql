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
WHERE p.club_id = $1
  AND u.role = 'player'
  AND u.email IS NOT NULL
ORDER BY
    CASE u.status WHEN 'pending' THEN 0 ELSE 1 END,
    u.created_at DESC;

-- name: ListPlayerBriefsByIDs :many
SELECT u.id, u.first_name, u.last_name, COALESCE(p.level, '') AS level, COALESCE(p.slug, '') AS slug
FROM users u
LEFT JOIN profiles p ON p.user_id = u.id
WHERE u.id = ANY($1::uuid[]);

-- name: ListUserContactBriefsByIDs :many
SELECT id, email, first_name, last_name
FROM users
WHERE id = ANY($1::uuid[]);

-- name: UpdateUserPassword :one
UPDATE users
SET password_hash = $2, updated_at = now()
WHERE id = $1
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: UpdateUserNames :one
UPDATE users
SET first_name = $2,
    last_name = $3,
    updated_at = now()
WHERE id = $1
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: CreatePlaceholderUser :one
-- Organizer-created player without email/login; claim later via manual merge.
INSERT INTO users (
    email, password_hash, first_name, last_name, role, status
) VALUES (
    NULL, $1, $2, $3, $4, $5
)
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: ClearUserEmail :exec
-- Free the unique email index before claiming onto a placeholder.
UPDATE users
SET email = NULL, updated_at = now()
WHERE id = $1
  AND email IS NOT NULL;

-- name: ClaimPlaceholderFromUser :one
-- Absorb credentials from a real account onto a placeholder (email IS NULL).
UPDATE users
SET email = sqlc.arg(email),
    password_hash = sqlc.arg(password_hash),
    email_verified_at = sqlc.narg(email_verified_at),
    first_name = sqlc.arg(first_name),
    last_name = sqlc.arg(last_name),
    status = 'approved',
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND email IS NULL
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: DeleteUserByID :exec
DELETE FROM users WHERE id = $1;

-- name: AnonymizeUserAccount :one
-- Right-to-erasure: strip identifiers, keep match FKs for tournament history.
UPDATE users
SET email = NULL,
    password_hash = $2,
    first_name = 'Удалённый',
    last_name = 'аккаунт',
    status = 'rejected',
    email_verified_at = NULL,
    updated_at = now()
WHERE id = $1
RETURNING id, email, password_hash, first_name, last_name, role, status,
          email_verified_at, created_at, updated_at;

-- name: ListPlaceholderPlayers :many
SELECT
    u.id,
    u.email,
    u.first_name,
    u.last_name,
    u.role,
    u.status AS user_status,
    p.slug,
    p.level,
    p.phone,
    p.verification_status,
    p.rating,
    p.points,
    p.created_at
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = $1
  AND u.email IS NULL
  AND u.role = 'player'
  AND u.status = 'approved'
  AND p.player_status = 'active'
ORDER BY u.last_name ASC, u.first_name ASC;
