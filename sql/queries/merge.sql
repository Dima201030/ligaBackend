-- Queries used when organizer merges a placeholder player into a real account.
-- Target (placeholder) keeps its id so tournament/rating FKs stay valid.

-- name: ReassignRegistrationPlayer :exec
UPDATE registrations
SET player_id = sqlc.arg(to_user_id)
WHERE player_id = sqlc.arg(from_user_id);

-- name: ReassignRegistrationPartner :exec
UPDATE registrations
SET partner_id = sqlc.arg(to_user_id)
WHERE partner_id = sqlc.arg(from_user_id);

-- name: ReassignMatchSidePlayer :exec
UPDATE match_sides
SET player_id = sqlc.arg(to_user_id)
WHERE player_id = sqlc.arg(from_user_id);

-- name: ReassignMatchSidePartner :exec
UPDATE match_sides
SET partner_id = sqlc.arg(to_user_id)
WHERE partner_id = sqlc.arg(from_user_id);

-- name: ReassignRatingEventPlayer :exec
UPDATE rating_events
SET player_id = sqlc.arg(to_user_id)
WHERE player_id = sqlc.arg(from_user_id);

-- name: ReassignRankingPlayer :exec
UPDATE rankings
SET player_id = sqlc.arg(to_user_id)
WHERE player_id = sqlc.arg(from_user_id);

-- name: GetOpenInviteTokenForUser :one
SELECT id, user_id, kind, token_hash, expires_at, used_at, created_at
FROM verification_tokens
WHERE user_id = $1
  AND kind = 'invite'
  AND used_at IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: MarkInviteTokensUsed :exec
UPDATE verification_tokens
SET used_at = now()
WHERE user_id = $1
  AND kind = 'invite'
  AND used_at IS NULL;
