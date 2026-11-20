-- +goose Up
-- +goose StatementBegin
CREATE TYPE match_status AS ENUM (
    'scheduled',
    'live',
    'completed',
    'cancelled',
    'walkover',
    'bye'
);

CREATE TYPE bracket_status AS ENUM (
    'none',
    'draft',
    'published'
);

ALTER TABLE tournaments
    ADD COLUMN bracket_status bracket_status NOT NULL DEFAULT 'none',
    ADD COLUMN bracket_published_at TIMESTAMPTZ,
    ADD COLUMN bracket_version INT NOT NULL DEFAULT 0,
    ADD COLUMN bracket_third_place BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE tournaments
    ADD CONSTRAINT tournaments_bracket_version_chk CHECK (bracket_version >= 0);

CREATE TABLE matches (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tournament_id       UUID NOT NULL REFERENCES tournaments (id) ON DELETE CASCADE,
    bracket_key         TEXT NOT NULL,
    stage               TEXT NOT NULL DEFAULT 'main',
    round_index         INT NOT NULL DEFAULT 0,
    position            INT NOT NULL DEFAULT 0,
    round_label         TEXT NOT NULL DEFAULT '',
    scheduled_at        TIMESTAMPTZ,
    court               TEXT,
    status              match_status NOT NULL DEFAULT 'scheduled',
    score_sets          JSONB,
    score_text          TEXT,
    winner_side         SMALLINT,
    next_match_id       UUID REFERENCES matches (id) ON DELETE SET NULL,
    next_slot           SMALLINT,
    loser_next_match_id UUID REFERENCES matches (id) ON DELETE SET NULL,
    loser_next_slot     SMALLINT,
    is_bye              BOOLEAN NOT NULL DEFAULT false,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT matches_tournament_key_uidx UNIQUE (tournament_id, bracket_key),
    CONSTRAINT matches_winner_side_chk CHECK (winner_side IS NULL OR winner_side IN (1, 2)),
    CONSTRAINT matches_next_slot_chk CHECK (next_slot IS NULL OR next_slot IN (1, 2)),
    CONSTRAINT matches_loser_next_slot_chk CHECK (loser_next_slot IS NULL OR loser_next_slot IN (1, 2)),
    CONSTRAINT matches_version_chk CHECK (version >= 1),
    CONSTRAINT matches_round_index_chk CHECK (round_index >= 0),
    CONSTRAINT matches_position_chk CHECK (position >= 0)
);

CREATE INDEX matches_tournament_id_idx ON matches (tournament_id);
CREATE INDEX matches_tournament_status_idx ON matches (tournament_id, status);
CREATE INDEX matches_next_match_id_idx ON matches (next_match_id);
CREATE INDEX matches_scheduled_at_idx ON matches (scheduled_at);

CREATE TABLE match_sides (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id         UUID NOT NULL REFERENCES matches (id) ON DELETE CASCADE,
    side             SMALLINT NOT NULL,
    player_id        UUID REFERENCES users (id) ON DELETE SET NULL,
    partner_id       UUID REFERENCES users (id) ON DELETE SET NULL,
    registration_id  UUID REFERENCES registrations (id) ON DELETE SET NULL,
    seed             INT,
    label            TEXT,
    is_bye           BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT match_sides_match_side_uidx UNIQUE (match_id, side),
    CONSTRAINT match_sides_side_chk CHECK (side IN (1, 2)),
    CONSTRAINT match_sides_seed_chk CHECK (seed IS NULL OR seed >= 1)
);

CREATE INDEX match_sides_match_id_idx ON match_sides (match_id);
CREATE INDEX match_sides_player_id_idx ON match_sides (player_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS match_sides;
DROP TABLE IF EXISTS matches;
ALTER TABLE tournaments
    DROP CONSTRAINT IF EXISTS tournaments_bracket_version_chk,
    DROP COLUMN IF EXISTS bracket_third_place,
    DROP COLUMN IF EXISTS bracket_version,
    DROP COLUMN IF EXISTS bracket_published_at,
    DROP COLUMN IF EXISTS bracket_status;
DROP TYPE IF EXISTS bracket_status;
DROP TYPE IF EXISTS match_status;
-- +goose StatementEnd
