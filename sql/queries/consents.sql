-- name: CreateUserConsent :one
INSERT INTO user_consents (user_id, kind, version, granted, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, user_id, kind, version, granted, ip, user_agent, created_at;

-- name: ListUserConsents :many
SELECT id, user_id, kind, version, granted, ip, user_agent, created_at
FROM user_consents
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: HasGrantedConsent :one
SELECT EXISTS (
    SELECT 1
    FROM user_consents c
    WHERE c.user_id = $1
      AND c.kind = $2
      AND c.granted = TRUE
      AND NOT EXISTS (
          SELECT 1
          FROM user_consents w
          WHERE w.user_id = c.user_id
            AND w.kind = c.kind
            AND w.granted = FALSE
            AND w.created_at > c.created_at
      )
) AS granted;
