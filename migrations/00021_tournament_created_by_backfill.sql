-- +goose Up
-- Backfill tournaments.created_by from the earliest tournament.create audit entry.
UPDATE tournaments AS t
SET created_by = a.actor_id,
    updated_at = now()
FROM (
    SELECT DISTINCT ON (entity_id)
        entity_id,
        actor_id
    FROM audit_log
    WHERE action = 'tournament.create'
      AND entity = 'tournament'
      AND entity_id IS NOT NULL
      AND actor_id IS NOT NULL
    ORDER BY entity_id, created_at ASC
) AS a
WHERE t.created_by IS NULL
  AND a.entity_id = t.id::text;

-- +goose Down
-- Irreversible data backfill; no schema change to undo.
