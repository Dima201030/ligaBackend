-- +goose Up
-- +goose StatementBegin
ALTER TABLE rating_events
    ADD COLUMN discipline TEXT NOT NULL DEFAULT 'singles';

ALTER TABLE rating_events
    ADD CONSTRAINT rating_events_discipline_chk CHECK (discipline IN ('singles', 'doubles'));

UPDATE rating_events e
SET discipline = CASE
    WHEN t.type::text IN ('doubles', 'mixed', 'teams') THEN 'doubles'
    ELSE 'singles'
END
FROM tournaments t
WHERE e.tournament_id = t.id
  AND e.kind = 'tournament';

CREATE INDEX rating_events_discipline_idx ON rating_events (discipline)
    WHERE kind = 'tournament';

ALTER TABLE profiles
    ADD COLUMN singles_points INT NOT NULL DEFAULT 0,
    ADD COLUMN doubles_points INT NOT NULL DEFAULT 0;

ALTER TABLE profiles
    ADD CONSTRAINT profiles_singles_points_chk CHECK (singles_points >= 0),
    ADD CONSTRAINT profiles_doubles_points_chk CHECK (doubles_points >= 0);

-- Until the next window recalc, treat existing totals as singles.
UPDATE profiles
SET singles_points = points,
    doubles_points = 0
WHERE singles_points = 0 AND doubles_points = 0 AND points > 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE profiles
    DROP CONSTRAINT IF EXISTS profiles_singles_points_chk,
    DROP CONSTRAINT IF EXISTS profiles_doubles_points_chk;

ALTER TABLE profiles
    DROP COLUMN IF EXISTS singles_points,
    DROP COLUMN IF EXISTS doubles_points;

DROP INDEX IF EXISTS rating_events_discipline_idx;

ALTER TABLE rating_events
    DROP CONSTRAINT IF EXISTS rating_events_discipline_chk;

ALTER TABLE rating_events
    DROP COLUMN IF EXISTS discipline;
-- +goose StatementEnd
