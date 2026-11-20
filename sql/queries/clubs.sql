-- name: CreateClub :one
INSERT INTO clubs (slug, name, city, status, settings)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetClubByID :one
SELECT * FROM clubs WHERE id = $1;

-- name: GetClubBySlug :one
SELECT * FROM clubs WHERE slug = $1;

-- name: GetActiveClubBySlug :one
SELECT * FROM clubs WHERE slug = $1 AND status = 'active';

-- name: ListActiveClubs :many
SELECT * FROM clubs
WHERE status = 'active'
ORDER BY name ASC;

-- name: ListClubs :many
SELECT * FROM clubs
ORDER BY name ASC;

-- name: UpdateClub :one
UPDATE clubs
SET name = $2,
    city = $3,
    status = $4,
    settings = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClubSlugExists :one
SELECT EXISTS(SELECT 1 FROM clubs WHERE slug = $1)::bool AS exists;

-- name: CreateClubMembership :one
INSERT INTO club_memberships (club_id, user_id, role, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetClubMembership :one
SELECT * FROM club_memberships
WHERE club_id = $1 AND user_id = $2;

-- name: ListUserClubMemberships :many
SELECT m.*, c.slug AS club_slug, c.name AS club_name, c.status AS club_status
FROM club_memberships m
JOIN clubs c ON c.id = m.club_id
WHERE m.user_id = $1
  AND m.status = 'active'
ORDER BY c.name ASC;

-- name: ListClubMembers :many
SELECT m.*, u.email, u.first_name, u.last_name, u.status AS user_status
FROM club_memberships m
JOIN users u ON u.id = m.user_id
WHERE m.club_id = $1
  AND (sqlc.narg(status)::text IS NULL OR m.status = sqlc.narg(status)::club_member_status)
ORDER BY m.created_at DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);
