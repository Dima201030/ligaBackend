-- name: CreateProfile :one
INSERT INTO profiles (user_id, phone, level)
VALUES ($1, $2, $3)
RETURNING user_id, phone, level, created_at, updated_at;

-- name: GetProfileByUserID :one
SELECT user_id, phone, level, created_at, updated_at
FROM profiles
WHERE user_id = $1;

-- name: UpdateProfileLevelAndNames :exec
UPDATE profiles
SET level = $2, updated_at = now()
WHERE user_id = $1;
