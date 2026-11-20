-- +goose Up
-- +goose StatementBegin
CREATE TYPE tournament_status AS ENUM (
    'draft',
    'registration_soon',
    'registration_open',
    'registration_closed',
    'scheduled',
    'live',
    'completed',
    'cancelled'
);

CREATE TYPE tournament_type AS ENUM (
    'singles',
    'doubles',
    'mixed',
    'final',
    'cup',
    'teams'
);

CREATE TYPE tournament_format AS ENUM (
    'olympic',
    'round_robin',
    'groups_playoff',
    'double_elimination'
);

CREATE TYPE surface_type AS ENUM ('hard', 'clay', 'grass', 'carpet');

CREATE TYPE registration_status AS ENUM (
    'pending',
    'confirmed',
    'waitlist',
    'cancelled',
    'rejected'
);

CREATE TABLE tournaments (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                    TEXT NOT NULL,
    title                   TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    type                    tournament_type NOT NULL,
    category                TEXT NOT NULL,
    surface                 surface_type NOT NULL DEFAULT 'hard',
    format                  tournament_format NOT NULL,
    status                  tournament_status NOT NULL DEFAULT 'draft',
    starts_at               TIMESTAMPTZ NOT NULL,
    ends_at                 TIMESTAMPTZ,
    registration_opens_at   TIMESTAMPTZ NOT NULL,
    registration_closes_at  TIMESTAMPTZ NOT NULL,
    venue_name              TEXT NOT NULL,
    venue_address           TEXT NOT NULL DEFAULT '',
    venue_courts            INT NOT NULL DEFAULT 1,
    venue_surface           surface_type NOT NULL DEFAULT 'hard',
    entry_fee               INT NOT NULL DEFAULT 0,
    max_participants        INT NOT NULL,
    cover_url               TEXT,
    season                  INT NOT NULL,
    created_by              UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tournaments_slug_uidx UNIQUE (slug),
    CONSTRAINT tournaments_title_chk CHECK (char_length(trim(title)) >= 3),
    CONSTRAINT tournaments_max_participants_chk CHECK (max_participants >= 2 AND max_participants <= 256),
    CONSTRAINT tournaments_entry_fee_chk CHECK (entry_fee >= 0),
    CONSTRAINT tournaments_venue_courts_chk CHECK (venue_courts >= 1 AND venue_courts <= 64),
    CONSTRAINT tournaments_category_chk CHECK (
        category IN ('masters', 'challenger', 'futures', 'rookies', 'beginners', 'newcomers')
    ),
    CONSTRAINT tournaments_reg_before_start_chk CHECK (registration_closes_at <= starts_at),
    CONSTRAINT tournaments_opens_before_closes_chk CHECK (registration_opens_at <= registration_closes_at)
);

CREATE INDEX tournaments_starts_at_idx ON tournaments (starts_at);
CREATE INDEX tournaments_status_idx ON tournaments (status);
CREATE INDEX tournaments_season_idx ON tournaments (season);

CREATE TABLE tournament_rule_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tournament_id   UUID NOT NULL REFERENCES tournaments (id) ON DELETE CASCADE,
    version         INT NOT NULL,
    rules           JSONB NOT NULL DEFAULT '{}'::jsonb,
    text_markdown   TEXT NOT NULL DEFAULT '',
    created_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tournament_rule_versions_uidx UNIQUE (tournament_id, version),
    CONSTRAINT tournament_rule_versions_version_chk CHECK (version >= 1)
);

CREATE INDEX tournament_rule_versions_tournament_id_idx
    ON tournament_rule_versions (tournament_id);

CREATE TABLE registrations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tournament_id       UUID NOT NULL REFERENCES tournaments (id) ON DELETE CASCADE,
    player_id           UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    partner_id          UUID REFERENCES users (id) ON DELETE SET NULL,
    looking_for_partner BOOLEAN NOT NULL DEFAULT false,
    status              registration_status NOT NULL DEFAULT 'pending',
    seed                INT,
    waitlist_position   INT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at        TIMESTAMPTZ,
    cancelled_at        TIMESTAMPTZ,
    CONSTRAINT registrations_tournament_player_uidx UNIQUE (tournament_id, player_id),
    CONSTRAINT registrations_no_self_partner_chk CHECK (
        partner_id IS NULL OR partner_id <> player_id
    ),
    CONSTRAINT registrations_partner_xor_looking_chk CHECK (
        NOT (partner_id IS NOT NULL AND looking_for_partner)
    ),
    CONSTRAINT registrations_seed_chk CHECK (seed IS NULL OR seed >= 1),
    CONSTRAINT registrations_waitlist_position_chk CHECK (
        waitlist_position IS NULL OR waitlist_position >= 1
    )
);

CREATE INDEX registrations_tournament_id_idx ON registrations (tournament_id);
CREATE INDEX registrations_player_id_idx ON registrations (player_id);
CREATE INDEX registrations_tournament_status_idx ON registrations (tournament_id, status);
CREATE UNIQUE INDEX registrations_active_partner_uidx
    ON registrations (tournament_id, partner_id)
    WHERE partner_id IS NOT NULL
      AND status IN ('pending', 'confirmed', 'waitlist');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS registrations;
DROP TABLE IF EXISTS tournament_rule_versions;
DROP TABLE IF EXISTS tournaments;
DROP TYPE IF EXISTS registration_status;
DROP TYPE IF EXISTS surface_type;
DROP TYPE IF EXISTS tournament_format;
DROP TYPE IF EXISTS tournament_type;
DROP TYPE IF EXISTS tournament_status;
-- +goose StatementEnd
