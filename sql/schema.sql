CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE user_role AS ENUM ('player', 'organizer', 'owner', 'superadmin');
CREATE TYPE user_status AS ENUM ('pending', 'approved', 'rejected');

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL,
    password_hash   TEXT NOT NULL,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    role            user_role NOT NULL DEFAULT 'player',
    status          user_status NOT NULL DEFAULT 'pending',
    email_verified_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_lower_chk CHECK (email = lower(email))
);

CREATE UNIQUE INDEX users_email_uidx ON users (email);

CREATE TABLE sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash      BYTEA NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    idle_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip              INET,
    user_agent      TEXT
);

CREATE UNIQUE INDEX sessions_token_hash_uidx ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_active_idx ON sessions (expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE profiles (
    user_id     UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    phone       TEXT,
    level       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT profiles_level_chk CHECK (
        level IN ('masters', 'challenger', 'futures', 'rookies', 'beginners', 'newcomers')
    )
);

CREATE TYPE verification_kind AS ENUM ('email', 'reset', 'invite');

CREATE TABLE verification_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind        verification_kind NOT NULL,
    token_hash  BYTEA NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX verification_tokens_hash_uidx ON verification_tokens (token_hash);
CREATE INDEX verification_tokens_user_id_idx ON verification_tokens (user_id);

CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    actor_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    action      TEXT NOT NULL,
    entity      TEXT NOT NULL,
    entity_id   TEXT,
    diff        JSONB,
    ip          INET,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_created_at_idx ON audit_log (created_at DESC);
CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id);

CREATE TYPE email_outbox_status AS ENUM ('pending', 'sent', 'failed', 'cancelled');

CREATE TABLE email_outbox (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "to"             TEXT NOT NULL,
    template         TEXT NOT NULL,
    payload          JSONB NOT NULL DEFAULT '{}'::jsonb,
    status           email_outbox_status NOT NULL DEFAULT 'pending',
    attempts         INT NOT NULL DEFAULT 0,
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at          TIMESTAMPTZ,
    error            TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX email_outbox_pending_idx ON email_outbox (next_attempt_at)
    WHERE status = 'pending';

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
    bracket_status          bracket_status NOT NULL DEFAULT 'none',
    bracket_published_at    TIMESTAMPTZ,
    bracket_version         INT NOT NULL DEFAULT 0,
    bracket_third_place     BOOLEAN NOT NULL DEFAULT false,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tournaments_slug_uidx UNIQUE (slug),
    CONSTRAINT tournaments_title_chk CHECK (char_length(trim(title)) >= 3),
    CONSTRAINT tournaments_max_participants_chk CHECK (max_participants >= 2 AND max_participants <= 256),
    CONSTRAINT tournaments_entry_fee_chk CHECK (entry_fee >= 0),
    CONSTRAINT tournaments_venue_courts_chk CHECK (venue_courts >= 1 AND venue_courts <= 64),
    CONSTRAINT tournaments_bracket_version_chk CHECK (bracket_version >= 0),
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
