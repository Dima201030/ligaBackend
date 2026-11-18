-- name: CreateVerificationToken :one
INSERT INTO verification_tokens (user_id, kind, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, kind, token_hash, expires_at, used_at, created_at;

-- name: GetVerificationTokenByHash :one
SELECT id, user_id, kind, token_hash, expires_at, used_at, created_at
FROM verification_tokens
WHERE token_hash = $1;

-- name: MarkVerificationTokenUsed :one
UPDATE verification_tokens
SET used_at = now()
WHERE id = $1 AND used_at IS NULL AND expires_at > now()
RETURNING id, user_id, kind, token_hash, expires_at, used_at, created_at;
