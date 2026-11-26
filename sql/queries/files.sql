-- name: CreateFile :one
INSERT INTO files (
    club_id, bucket, path, original_name, mime, size, width, height, checksum, uploaded_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: GetFileByID :one
SELECT *
FROM files
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetFileByClubAndID :one
SELECT *
FROM files
WHERE club_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteFile :one
UPDATE files
SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SetTournamentCover :one
UPDATE tournaments
SET cover_file_id = $3,
    cover_url = $4,
    updated_at = now()
WHERE club_id = $1 AND id = $2
RETURNING *;

-- name: ClearTournamentCover :one
UPDATE tournaments
SET cover_file_id = NULL,
    cover_url = NULL,
    updated_at = now()
WHERE club_id = $1 AND id = $2
RETURNING *;

-- name: SetNewsCover :one
UPDATE news_posts
SET cover_file_id = $2,
    cover_url = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetEventCover :one
UPDATE club_events
SET cover_file_id = $2,
    cover_url = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetProfileAvatar :one
UPDATE profiles
SET avatar_file_id = $2,
    avatar_url = $3,
    updated_at = now()
WHERE user_id = $1
RETURNING *;
