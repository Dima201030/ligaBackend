-- +goose Up
-- +goose StatementBegin
ALTER TABLE tournament_places
    DROP CONSTRAINT IF EXISTS tournament_places_place_check;

ALTER TABLE tournament_places
    ADD CONSTRAINT tournament_places_place_check CHECK (place BETWEEN 1 AND 4);

-- Singles shared-bronze workaround stuffed two players into place 3.
-- Split the second player onto place 4 when that slot is free.
WITH shared AS (
    SELECT
        tp.club_id,
        tp.tournament_id,
        tp.player_ids[1] AS third_id,
        tp.player_ids[2] AS fourth_id
    FROM tournament_places tp
    JOIN tournaments t ON t.id = tp.tournament_id
    WHERE tp.place = 3
      AND cardinality(tp.player_ids) = 2
      AND t.type = 'singles'
      AND NOT EXISTS (
          SELECT 1
          FROM tournament_places other
          WHERE other.tournament_id = tp.tournament_id
            AND other.place = 4
      )
),
trimmed AS (
    UPDATE tournament_places AS tp
    SET player_ids = ARRAY[shared.third_id],
        updated_at = now()
    FROM shared
    WHERE tp.tournament_id = shared.tournament_id
      AND tp.place = 3
    RETURNING tp.tournament_id
)
INSERT INTO tournament_places (club_id, tournament_id, place, player_ids)
SELECT club_id, tournament_id, 4, ARRAY[fourth_id]
FROM shared;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
WITH fourth AS (
    SELECT club_id, tournament_id, player_ids[1] AS fourth_id
    FROM tournament_places
    WHERE place = 4
      AND cardinality(player_ids) >= 1
),
merged AS (
    UPDATE tournament_places AS tp
    SET player_ids = CASE
            WHEN cardinality(tp.player_ids) = 0 THEN ARRAY[fourth.fourth_id]
            WHEN cardinality(tp.player_ids) = 1 THEN tp.player_ids || fourth.fourth_id
            ELSE tp.player_ids
        END,
        updated_at = now()
    FROM fourth
    WHERE tp.tournament_id = fourth.tournament_id
      AND tp.place = 3
      AND cardinality(tp.player_ids) <= 1
    RETURNING tp.tournament_id
)
DELETE FROM tournament_places tp
USING fourth
WHERE tp.tournament_id = fourth.tournament_id
  AND tp.place = 4;

DELETE FROM tournament_places WHERE place = 4;

ALTER TABLE tournament_places
    DROP CONSTRAINT IF EXISTS tournament_places_place_check;

ALTER TABLE tournament_places
    ADD CONSTRAINT tournament_places_place_check CHECK (place BETWEEN 1 AND 3);
-- +goose StatementEnd
