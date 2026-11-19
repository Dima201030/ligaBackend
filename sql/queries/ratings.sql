-- name: GetLatestRatingConfig :one
SELECT id, season, version, coefficients, active_from, created_by, created_at
FROM rating_configs
WHERE season = $1
ORDER BY version DESC
LIMIT 1;

-- name: GetRatingConfigByID :one
SELECT id, season, version, coefficients, active_from, created_by, created_at
FROM rating_configs
WHERE id = $1;

-- name: InsertRatingEvent :one
INSERT INTO rating_events (
    player_id, match_id, tournament_id, kind, discipline,
    points_delta, rating_delta, rating_after, points_after,
    config_id, reason, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: RatingEventExistsForMatch :one
SELECT EXISTS(
    SELECT 1 FROM rating_events
    WHERE match_id = $1 AND player_id = $2 AND kind = 'match'
)::bool AS exists;

-- name: RatingEventExistsForTournament :one
SELECT EXISTS(
    SELECT 1 FROM rating_events
    WHERE tournament_id = $1 AND player_id = $2 AND kind = 'tournament'
)::bool AS exists;

-- name: ListRatingEventsByPlayer :many
SELECT id, player_id, match_id, tournament_id, kind, discipline,
       points_delta, rating_delta, rating_after, points_after,
       config_id, reason, created_by, created_at
FROM rating_events
WHERE player_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListTournamentPointEventsInWindow :many
SELECT player_id, points_delta, tournament_id, discipline, created_at
FROM rating_events
WHERE kind = 'tournament'
  AND created_at >= $1
ORDER BY player_id, points_delta DESC;

-- name: ApplyProfileRatingDelta :one
UPDATE profiles
SET rating = sqlc.arg(rating),
    points = sqlc.arg(points),
    matches_played = matches_played + sqlc.arg(matches_inc),
    wins = wins + sqlc.arg(wins_inc),
    losses = losses + sqlc.arg(losses_inc),
    updated_at = now()
WHERE user_id = sqlc.arg(user_id)
RETURNING *;

-- name: SetProfilePoints :one
UPDATE profiles
SET points = $2, updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: SetProfilePointsBreakdown :one
UPDATE profiles
SET singles_points = sqlc.arg(singles_points),
    doubles_points = sqlc.arg(doubles_points),
    points = sqlc.arg(singles_points)::int + sqlc.arg(doubles_points)::int,
    updated_at = now()
WHERE user_id = sqlc.arg(user_id)
RETURNING *;

-- name: LockProfileForUpdate :one
SELECT user_id, phone, level, slug, gender, city, bio, avatar_url, avatar_file_id, player_status,
       rating, points, singles_points, doubles_points, matches_played, wins, losses, created_at, updated_at
FROM profiles
WHERE user_id = $1
FOR UPDATE;

-- name: InsertRankingRow :one
INSERT INTO rankings (
    season, player_id, position, previous_position,
    points, rating, singles_points, doubles_points, category, calculated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: LatestRankingCalculatedAt :one
SELECT calculated_at
FROM rankings
WHERE rankings.season = $1
ORDER BY calculated_at DESC
LIMIT 1;

-- name: ListLatestRankings :many
SELECT r.id, r.season, r.player_id, r.position, r.previous_position,
       r.points, r.rating, r.singles_points, r.doubles_points, r.category, r.calculated_at,
       u.first_name, u.last_name, p.slug, p.level, p.player_status, p.avatar_url
FROM rankings r
JOIN users u ON u.id = r.player_id
JOIN profiles p ON p.user_id = r.player_id
WHERE r.season = $1
  AND r.calculated_at = (
      SELECT r2.calculated_at FROM rankings r2 WHERE r2.season = $1 ORDER BY r2.calculated_at DESC LIMIT 1
  )
  AND (sqlc.narg(category)::text IS NULL OR r.category = sqlc.narg(category))
ORDER BY r.position ASC;

-- name: ListPreviousPositions :many
SELECT player_id, position
FROM rankings
WHERE rankings.season = $1
  AND calculated_at = (
      SELECT r2.calculated_at FROM rankings r2
      WHERE r2.season = $1
        AND r2.calculated_at < $2
      ORDER BY r2.calculated_at DESC
      LIMIT 1
  );

-- name: CountMainDrawWins :one
SELECT COUNT(*)::int AS wins
FROM matches m
JOIN match_sides ms ON ms.match_id = m.id
WHERE m.tournament_id = $1
  AND m.stage = 'main'
  AND m.status IN ('completed', 'walkover')
  AND m.is_bye = false
  AND ms.side = m.winner_side
  AND (ms.player_id = $2 OR ms.partner_id = $2);

-- name: CountConfirmedParticipants :one
SELECT COUNT(*)::int AS count
FROM registrations
WHERE tournament_id = $1 AND status = 'confirmed';

-- name: ListConfirmedPlayerIDs :many
SELECT DISTINCT pid AS player_id
FROM (
    SELECT r.player_id AS pid
    FROM registrations r
    WHERE r.tournament_id = $1 AND r.status = 'confirmed'
    UNION ALL
    SELECT r.partner_id AS pid
    FROM registrations r
    WHERE r.tournament_id = $1
      AND r.status = 'confirmed'
      AND r.partner_id IS NOT NULL
) AS players;
