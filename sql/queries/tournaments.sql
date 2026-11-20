-- name: CreateTournament :one
INSERT INTO tournaments (
    club_id, slug, title, description, type, category, surface, format, status,
    starts_at, ends_at, registration_opens_at, registration_closes_at,
    venue_name, venue_address, venue_courts, venue_surface,
    entry_fee, max_participants, cover_url, season, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13,
    $14, $15, $16, $17,
    $18, $19, $20, $21, $22
)
RETURNING *;

-- name: UpdateTournament :one
UPDATE tournaments SET
    title = $2,
    description = $3,
    type = $4,
    category = $5,
    surface = $6,
    format = $7,
    status = $8,
    starts_at = $9,
    ends_at = $10,
    registration_opens_at = $11,
    registration_closes_at = $12,
    venue_name = $13,
    venue_address = $14,
    venue_courts = $15,
    venue_surface = $16,
    entry_fee = $17,
    max_participants = $18,
    cover_url = $19,
    season = $20,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetTournamentByID :one
SELECT * FROM tournaments WHERE id = $1;

-- name: GetTournamentByClubAndID :one
SELECT * FROM tournaments WHERE club_id = $1 AND id = $2;

-- name: GetTournamentBySlug :one
SELECT * FROM tournaments WHERE club_id = $1 AND slug = $2;

-- name: LockTournamentByID :one
SELECT * FROM tournaments WHERE id = $1 FOR UPDATE;

-- name: CompleteTournamentIfActive :one
UPDATE tournaments
SET status = 'completed',
    updated_at = now()
WHERE id = $1
  AND status NOT IN ('completed', 'cancelled', 'draft')
RETURNING *;

-- name: MarkTournamentLiveIfPending :one
UPDATE tournaments
SET status = 'live',
    updated_at = now()
WHERE id = $1
  AND status IN (
      'registration_soon',
      'registration_open',
      'registration_closed',
      'scheduled'
  )
RETURNING *;

-- name: ListPublicTournaments :many
SELECT * FROM tournaments
WHERE club_id = $1
  AND status <> 'draft'
ORDER BY starts_at ASC;

-- name: ListOrganizerTournaments :many
SELECT * FROM tournaments
WHERE club_id = $1
ORDER BY
    CASE status
        WHEN 'live' THEN 0
        WHEN 'registration_open' THEN 1
        WHEN 'registration_soon' THEN 2
        WHEN 'registration_closed' THEN 3
        WHEN 'scheduled' THEN 4
        WHEN 'draft' THEN 5
        WHEN 'completed' THEN 6
        WHEN 'cancelled' THEN 7
        ELSE 8
    END,
    starts_at ASC;

-- name: SlugExists :one
SELECT EXISTS(
    SELECT 1 FROM tournaments WHERE club_id = $1 AND slug = $2
)::bool AS exists;

-- name: CreateTournamentRuleVersion :one
INSERT INTO tournament_rule_versions (
    tournament_id, version, rules, text_markdown, created_by
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: LatestTournamentRuleVersion :one
SELECT * FROM tournament_rule_versions
WHERE tournament_id = $1
ORDER BY version DESC
LIMIT 1;

-- name: NextRuleVersion :one
SELECT COALESCE(MAX(version), 0) + 1 AS next_version
FROM tournament_rule_versions
WHERE tournament_id = $1;
