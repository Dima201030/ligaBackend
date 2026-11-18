-- name: EnqueueEmail :one
INSERT INTO email_outbox ("to", template, payload)
VALUES ($1, $2, $3)
RETURNING id, "to", template, payload, status, attempts, next_attempt_at, sent_at, error, created_at;
