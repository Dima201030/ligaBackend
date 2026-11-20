-- name: EnqueueEmail :one
INSERT INTO email_outbox ("to", template, payload)
VALUES ($1, $2, $3)
RETURNING id, "to", template, payload, status, attempts, next_attempt_at, sent_at, error, created_at;

-- name: ClaimPendingEmails :many
WITH due AS (
    SELECT o.id AS due_id
    FROM email_outbox AS o
    WHERE o.status = 'pending'
      AND o.next_attempt_at <= now()
      AND o.attempts < $1
    ORDER BY o.next_attempt_at ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE email_outbox AS e
SET
    attempts = e.attempts + 1,
    error = NULL,
    next_attempt_at = now() + ((GREATEST(e.attempts + 1, 1))::text || ' minutes')::interval
FROM due
WHERE e.id = due.due_id
RETURNING e.id, e."to", e.template, e.payload, e.status, e.attempts, e.next_attempt_at, e.sent_at, e.error, e.created_at;

-- name: MarkEmailSent :one
UPDATE email_outbox
SET status = 'sent',
    sent_at = now(),
    error = NULL,
    next_attempt_at = now()
WHERE id = $1
RETURNING id, "to", template, payload, status, attempts, next_attempt_at, sent_at, error, created_at;

-- name: MarkEmailFailed :one
UPDATE email_outbox
SET status = CASE
        WHEN attempts >= $2 THEN 'failed'::email_outbox_status
        ELSE 'pending'::email_outbox_status
    END,
    error = $3,
    next_attempt_at = now() + ((LEAST(attempts * 2, 60))::text || ' minutes')::interval
WHERE id = $1
RETURNING id, "to", template, payload, status, attempts, next_attempt_at, sent_at, error, created_at;

-- name: CountOutboxByStatus :one
SELECT COUNT(*)::int AS count
FROM email_outbox
WHERE status = $1;
