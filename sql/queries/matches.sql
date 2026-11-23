-- name: ListConfirmedRegistrationsForBracket :many
SELECT
    r.id,
    r.tournament_id,
    r.player_id,
    r.partner_id,
    r.looking_for_partner,
    r.status,
    r.seed,
    r.waitlist_position,
    r.created_at,
    r.confirmed_at,
    r.cancelled_at,
    pu.first_name AS player_first_name,
    pu.last_name AS player_last_name,
    pp.level AS player_level,
    pp.rating AS player_rating,
    pp.slug AS player_slug
FROM registrations r
JOIN users pu ON pu.id = r.player_id
JOIN profiles pp ON pp.user_id = r.player_id
WHERE r.tournament_id = $1
  AND r.status = 'confirmed'
ORDER BY r.seed ASC NULLS LAST, r.created_at ASC;

-- name: SetTournamentBracketDraft :one
UPDATE tournaments
SET bracket_status = 'draft',
    bracket_published_at = NULL,
    bracket_version = bracket_version + 1,
    bracket_third_place = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: PublishTournamentBracket :one
UPDATE tournaments
SET bracket_status = 'published',
    bracket_published_at = now(),
    updated_at = now()
WHERE id = $1
  AND bracket_status IN ('draft', 'published')
RETURNING *;

-- name: CountResultMatches :one
SELECT COUNT(*)::int AS count
FROM matches
WHERE tournament_id = $1
  AND status IN ('completed', 'walkover', 'disqualified');

-- name: CountOpenPlayableMatches :one
SELECT COUNT(*)::int AS count
FROM matches m
WHERE m.tournament_id = $1
  AND m.is_bye = false
  AND m.status IN ('scheduled', 'live')
  AND EXISTS (
      SELECT 1 FROM match_sides s1
      WHERE s1.match_id = m.id AND s1.side = 1 AND s1.player_id IS NOT NULL
  )
  AND EXISTS (
      SELECT 1 FROM match_sides s2
      WHERE s2.match_id = m.id AND s2.side = 2 AND s2.player_id IS NOT NULL
  );

-- name: DeleteMatchesByTournament :exec
DELETE FROM matches WHERE tournament_id = $1;

-- name: CreateMatch :one
INSERT INTO matches (
    tournament_id, bracket_key, stage, round_index, position, round_label,
    status, winner_side, is_bye, next_slot, loser_next_slot
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11
)
RETURNING *;

-- name: UpdateMatchLinks :one
UPDATE matches
SET next_match_id = $2,
    next_slot = $3,
    loser_next_match_id = $4,
    loser_next_slot = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateMatchSide :one
INSERT INTO match_sides (
    match_id, side, player_id, partner_id, registration_id, seed, label, is_bye
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetMatchByID :one
SELECT * FROM matches WHERE id = $1;

-- name: LockMatchByID :one
SELECT * FROM matches WHERE id = $1 FOR UPDATE;

-- name: GetMatchByTournamentKey :one
SELECT * FROM matches
WHERE tournament_id = $1 AND bracket_key = $2;

-- name: ListMatchesByTournament :many
SELECT * FROM matches
WHERE tournament_id = $1
ORDER BY
    CASE stage
        WHEN 'group' THEN 0
        WHEN 'main' THEN 1
        WHEN 'playoff' THEN 2
        WHEN 'lower' THEN 3
        WHEN 'third_place' THEN 4
        WHEN 'grand_final' THEN 5
        ELSE 6
    END,
    round_index ASC,
    position ASC;

-- name: ListMatchSidesByTournament :many
SELECT ms.*
FROM match_sides ms
JOIN matches m ON m.id = ms.match_id
WHERE m.tournament_id = $1
ORDER BY m.round_index ASC, m.position ASC, ms.side ASC;

-- name: ListMatchSidesByMatchIDs :many
SELECT *
FROM match_sides
WHERE match_id = ANY($1::uuid[])
ORDER BY match_id, side;

-- name: UpdateMatchSidePlayers :one
UPDATE match_sides
SET player_id = $3,
    partner_id = $4,
    registration_id = $5,
    seed = $6,
    label = $7,
    is_bye = $8
WHERE match_id = $1 AND side = $2
RETURNING *;

-- name: ClearMatchSidePlayers :one
UPDATE match_sides
SET player_id = NULL,
    partner_id = NULL,
    registration_id = NULL,
    seed = NULL,
    label = $3,
    is_bye = false
WHERE match_id = $1 AND side = $2
RETURNING *;

-- name: SubmitMatchResult :one
-- First finish: scheduled/live. Correction: completed/walkover/disqualified with matching version
-- (service layer refuses if the next-round match already has a result).
UPDATE matches
SET status = $2,
    score_sets = $3,
    score_text = $4,
    winner_side = $5,
    version = version + 1,
    updated_at = now()
WHERE id = $1
  AND version = $6
  AND status IN ('scheduled', 'live', 'completed', 'walkover', 'disqualified')
RETURNING *;

-- name: CancelMatch :one
UPDATE matches
SET status = 'cancelled',
    score_sets = NULL,
    score_text = NULL,
    winner_side = NULL,
    version = version + 1,
    updated_at = now()
WHERE id = $1
  AND version = $2
  AND status IN ('scheduled', 'live')
RETURNING *;

-- name: UpdateMatchSchedule :one
UPDATE matches
SET scheduled_at = $2,
    court = $3,
    updated_at = now()
WHERE id = $1
  AND status IN ('scheduled', 'live', 'bye')
RETURNING *;

-- name: SetMatchStatus :one
UPDATE matches
SET status = $2,
    updated_at = now()
WHERE id = $1
  AND status IN ('scheduled', 'live')
RETURNING *;

-- name: SetMatchBracketState :one
UPDATE matches
SET status = $2,
    is_bye = $3,
    winner_side = $4,
    version = version + 1,
    updated_at = now()
WHERE id = $1
  AND status IN ('scheduled', 'bye', 'live')
RETURNING *;

-- name: ListOrganizerMatches :many
SELECT
    m.*,
    t.slug AS tournament_slug,
    t.title AS tournament_title,
    t.type AS tournament_type
FROM matches m
JOIN tournaments t ON t.id = m.tournament_id
WHERE t.club_id = $1
  AND m.status <> 'bye'
ORDER BY
    CASE m.status
        WHEN 'live' THEN 0
        WHEN 'scheduled' THEN 1
        WHEN 'completed' THEN 2
        WHEN 'walkover' THEN 3
        WHEN 'disqualified' THEN 3
        ELSE 4
    END,
    m.scheduled_at ASC NULLS LAST,
    m.created_at ASC;

-- name: ListMatchIDsByPlayer :many
SELECT DISTINCT m.id
FROM matches m
JOIN match_sides ms ON ms.match_id = m.id
WHERE m.status <> 'bye'
  AND (ms.player_id = $1 OR ms.partner_id = $1)
ORDER BY m.id;
