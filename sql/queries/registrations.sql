-- name: CreateRegistration :one
INSERT INTO registrations (
    tournament_id, player_id, partner_id, looking_for_partner,
    status, waitlist_position, confirmed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetRegistrationByTournamentPlayer :one
SELECT * FROM registrations
WHERE tournament_id = $1 AND player_id = $2;

-- name: LockRegistrationByTournamentPlayer :one
SELECT * FROM registrations
WHERE tournament_id = $1 AND player_id = $2
FOR UPDATE;

-- name: CountOccupyingRegistrations :one
SELECT COUNT(*)::int AS count
FROM registrations
WHERE tournament_id = $1
  AND status IN ('pending', 'confirmed');

-- name: NextWaitlistPosition :one
SELECT COALESCE(MAX(waitlist_position), 0)::int + 1 AS next_position
FROM registrations
WHERE tournament_id = $1 AND status = 'waitlist';

-- name: PlayerAlreadyActiveInTournament :one
SELECT EXISTS(
    SELECT 1 FROM registrations
    WHERE tournament_id = $1
      AND status IN ('pending', 'confirmed', 'waitlist')
      AND (player_id = $2 OR partner_id = $2)
)::bool AS exists;

-- name: CancelRegistration :one
UPDATE registrations
SET status = 'cancelled',
    cancelled_at = now(),
    waitlist_position = NULL
WHERE tournament_id = $1
  AND player_id = $2
  AND status IN ('pending', 'confirmed', 'waitlist')
RETURNING *;

-- name: LockOldestWaitlist :one
SELECT * FROM registrations
WHERE tournament_id = $1 AND status = 'waitlist'
ORDER BY waitlist_position ASC NULLS LAST, created_at ASC
LIMIT 1
FOR UPDATE;

-- name: PromoteRegistration :one
UPDATE registrations
SET status = 'confirmed',
    confirmed_at = now(),
    waitlist_position = NULL
WHERE id = $1 AND status = 'waitlist'
RETURNING *;

-- name: ListRegistrationsByTournament :many
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
    partner_u.first_name AS partner_first_name,
    partner_u.last_name AS partner_last_name,
    partner_p.level AS partner_level
FROM registrations r
JOIN users pu ON pu.id = r.player_id
JOIN profiles pp ON pp.user_id = r.player_id
LEFT JOIN users partner_u ON partner_u.id = r.partner_id
LEFT JOIN profiles partner_p ON partner_p.user_id = r.partner_id
WHERE r.tournament_id = $1
  AND r.status <> 'cancelled'
ORDER BY
    CASE r.status
        WHEN 'confirmed' THEN 0
        WHEN 'pending' THEN 1
        WHEN 'waitlist' THEN 2
        ELSE 3
    END,
    r.seed ASC NULLS LAST,
    r.created_at ASC;

-- name: GetApprovedUserByID :one
SELECT id, email, password_hash, first_name, last_name, role, status,
       email_verified_at, created_at, updated_at
FROM users
WHERE id = $1 AND status = 'approved';
