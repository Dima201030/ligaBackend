-- +goose Up
-- +goose StatementBegin
CREATE TABLE tournament_places (
    club_id       UUID NOT NULL REFERENCES clubs (id) ON DELETE CASCADE,
    tournament_id UUID NOT NULL REFERENCES tournaments (id) ON DELETE CASCADE,
    place         SMALLINT NOT NULL CHECK (place BETWEEN 1 AND 3),
    player_ids    UUID[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tournament_id, place),
    CONSTRAINT tournament_places_players_chk CHECK (cardinality(player_ids) <= 2)
);

CREATE INDEX tournament_places_club_idx ON tournament_places (club_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS tournament_places;
-- +goose StatementEnd
