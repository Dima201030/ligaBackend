-- Club owner analytics aggregates (scoped by club_id).

-- name: CountClubApprovedActivePlayers :one
SELECT COUNT(*)::int AS count
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = $1
  AND p.player_status = 'active'
  AND u.status = 'approved'
  AND u.email IS NOT NULL;

-- name: CountClubPendingApplications :one
SELECT COUNT(*)::int AS count
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = $1
  AND u.role = 'player'
  AND u.status = 'pending'
  AND u.email IS NOT NULL;

-- name: CountClubPlaceholderPlayers :one
SELECT COUNT(*)::int AS count
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = $1
  AND u.email IS NULL
  AND p.player_status IN ('active', 'blocked');

-- name: CountClubActiveMembers :one
SELECT COUNT(*)::int AS count
FROM club_memberships
WHERE club_id = $1
  AND status = 'active';

-- name: CountClubTournaments :one
SELECT COUNT(*)::int AS count
FROM tournaments
WHERE club_id = $1;

-- name: CountClubTournamentsByStatus :many
SELECT status::text AS status, COUNT(*)::int AS count
FROM tournaments
WHERE club_id = $1
GROUP BY status
ORDER BY status;

-- name: CountClubRegistrationsByStatus :many
SELECT r.status::text AS status, COUNT(*)::int AS count
FROM registrations r
JOIN tournaments t ON t.id = r.tournament_id
WHERE t.club_id = $1
GROUP BY r.status
ORDER BY r.status;

-- name: CountClubMatchesByStatus :many
SELECT m.status::text AS status, COUNT(*)::int AS count
FROM matches m
JOIN tournaments t ON t.id = m.tournament_id
WHERE t.club_id = $1
  AND m.is_bye = false
GROUP BY m.status
ORDER BY m.status;

-- name: ListClubPlayerGrowthByMonth :many
SELECT to_char(date_trunc('month', p.created_at AT TIME ZONE 'UTC'), 'YYYY-MM') AS month,
       COUNT(*)::int AS count
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.club_id = $1
  AND u.email IS NOT NULL
  AND p.created_at >= $2
GROUP BY 1
ORDER BY 1;

-- name: ListClubRegistrationGrowthByMonth :many
SELECT to_char(date_trunc('month', r.created_at AT TIME ZONE 'UTC'), 'YYYY-MM') AS month,
       COUNT(*)::int AS count
FROM registrations r
JOIN tournaments t ON t.id = r.tournament_id
WHERE t.club_id = $1
  AND r.created_at >= $2
GROUP BY 1
ORDER BY 1;

-- name: ListClubCompletedMatchGrowthByMonth :many
SELECT to_char(date_trunc('month', m.updated_at AT TIME ZONE 'UTC'), 'YYYY-MM') AS month,
       COUNT(*)::int AS count
FROM matches m
JOIN tournaments t ON t.id = m.tournament_id
WHERE t.club_id = $1
  AND m.is_bye = false
  AND m.status IN ('completed', 'walkover', 'disqualified')
  AND m.updated_at >= $2
GROUP BY 1
ORDER BY 1;
