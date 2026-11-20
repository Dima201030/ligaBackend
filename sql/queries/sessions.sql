-- name: CreateSession :one
INSERT INTO sessions (
    user_id, token_hash, expires_at, idle_expires_at, ip, user_agent
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, user_id, token_hash, expires_at, idle_expires_at,
          revoked_at, created_at, last_seen_at, ip, user_agent;

-- name: GetSessionByTokenHash :one
SELECT id, user_id, token_hash, expires_at, idle_expires_at,
       revoked_at, created_at, last_seen_at, ip, user_agent
FROM sessions
WHERE token_hash = $1;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE sessions
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: TouchSession :one
UPDATE sessions
SET last_seen_at = now(),
    idle_expires_at = $2
WHERE id = $1
  AND revoked_at IS NULL
  AND expires_at > now()
  AND idle_expires_at > now()
RETURNING id, user_id, token_hash, expires_at, idle_expires_at,
          revoked_at, created_at, last_seen_at, ip, user_agent;
