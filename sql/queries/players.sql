-- name: CreateProfile :one
INSERT INTO profiles (
    user_id, club_id, phone, level, slug, gender, city, bio, avatar_url,
    player_status, rating, points, singles_points, doubles_points, matches_played, wins, losses
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16, $17
)
RETURNING *;

-- name: GetProfileByUserID :one
SELECT user_id, club_id, phone, level, slug, gender, city, bio, avatar_url, avatar_file_id, player_status,
       rating, points, singles_points, doubles_points, matches_played, wins, losses, created_at, updated_at
FROM profiles
WHERE user_id = $1;

-- name: GetProfileBySlug :one
SELECT user_id, club_id, phone, level, slug, gender, city, bio, avatar_url, avatar_file_id, player_status,
       rating, points, singles_points, doubles_points, matches_played, wins, losses, created_at, updated_at
FROM profiles
WHERE club_id = $1 AND slug = $2;

-- name: UpdateProfileLevelAndNames :exec
UPDATE profiles
SET level = $2, updated_at = now()
WHERE user_id = $1;

-- name: UpdateProfilePublic :one
UPDATE profiles
SET gender = COALESCE(sqlc.narg(gender), gender),
    city = COALESCE(sqlc.narg(city), city),
    bio = COALESCE(sqlc.narg(bio), bio),
    avatar_url = COALESCE(sqlc.narg(avatar_url), avatar_url),
    player_status = COALESCE(sqlc.narg(player_status), player_status),
    level = COALESCE(sqlc.narg(level), level),
    updated_at = now()
WHERE user_id = sqlc.arg(user_id)
RETURNING *;

-- name: ListPublicPlayers :many
SELECT
    u.id,
    u.first_name,
    u.last_name,
    u.status AS user_status,
    p.slug,
    p.level,
    p.gender,
    p.city,
    p.bio,
    p.avatar_url,
    p.player_status,
    p.rating,
    p.points,
    p.singles_points,
    p.doubles_points,
    p.matches_played,
    p.wins,
    p.losses,
    p.created_at,
    p.updated_at
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = sqlc.arg(club_id)
  AND u.status = 'approved'
  AND p.player_status = 'active'
  AND (sqlc.narg(category)::text IS NULL OR p.level = sqlc.narg(category))
  AND (
      sqlc.narg(query)::text IS NULL
      OR u.first_name ILIKE '%' || sqlc.narg(query) || '%'
      OR u.last_name ILIKE '%' || sqlc.narg(query) || '%'
      OR p.slug ILIKE '%' || sqlc.narg(query) || '%'
  )
ORDER BY p.points DESC, p.rating DESC, u.last_name ASC, u.first_name ASC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: GetPublicPlayerBySlug :one
SELECT
    u.id,
    u.first_name,
    u.last_name,
    u.status AS user_status,
    p.slug,
    p.level,
    p.gender,
    p.city,
    p.bio,
    p.avatar_url,
    p.player_status,
    p.rating,
    p.points,
    p.singles_points,
    p.doubles_points,
    p.matches_played,
    p.wins,
    p.losses,
    p.created_at,
    p.updated_at
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = $1
  AND p.slug = $2
  AND u.status = 'approved'
  AND p.player_status IN ('active', 'archived');

-- name: ListOrganizerPlayers :many
SELECT
    u.id,
    u.email,
    u.first_name,
    u.last_name,
    u.role,
    u.status AS user_status,
    p.slug,
    p.level,
    p.gender,
    p.city,
    p.bio,
    p.avatar_url,
    p.player_status,
    p.rating,
    p.points,
    p.singles_points,
    p.doubles_points,
    p.matches_played,
    p.wins,
    p.losses,
    p.phone,
    p.created_at,
    p.updated_at
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = sqlc.arg(club_id)
  AND (sqlc.narg(user_status)::text IS NULL OR u.status = sqlc.narg(user_status)::user_status)
  AND (sqlc.narg(category)::text IS NULL OR p.level = sqlc.narg(category))
ORDER BY u.created_at DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);
