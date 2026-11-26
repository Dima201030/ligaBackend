CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE user_role AS ENUM ('player', 'organizer', 'owner', 'superadmin');
CREATE TYPE user_status AS ENUM ('pending', 'approved', 'rejected');

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NULL email = organizer-created placeholder (no login until merged with a real account).
    email           TEXT,
    password_hash   TEXT NOT NULL,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    role            user_role NOT NULL DEFAULT 'player',
    status          user_status NOT NULL DEFAULT 'pending',
    email_verified_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_lower_chk CHECK (email IS NULL OR email = lower(email))
);

CREATE UNIQUE INDEX users_email_uidx ON users (email) WHERE email IS NOT NULL;

CREATE TYPE club_status AS ENUM ('active', 'suspended', 'archived');
CREATE TYPE club_member_role AS ENUM ('player', 'organizer', 'owner');
CREATE TYPE club_member_status AS ENUM ('active', 'invited', 'blocked', 'left');

CREATE TABLE clubs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    city        TEXT NOT NULL DEFAULT '',
    status      club_status NOT NULL DEFAULT 'active',
    settings    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT clubs_slug_uidx UNIQUE (slug),
    CONSTRAINT clubs_slug_chk CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    CONSTRAINT clubs_name_chk CHECK (char_length(trim(name)) >= 2)
);

CREATE INDEX clubs_status_idx ON clubs (status);

CREATE TABLE club_memberships (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id     UUID NOT NULL REFERENCES clubs (id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role        club_member_role NOT NULL DEFAULT 'player',
    status      club_member_status NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT club_memberships_club_user_uidx UNIQUE (club_id, user_id)
);

CREATE INDEX club_memberships_user_id_idx ON club_memberships (user_id);
CREATE INDEX club_memberships_club_status_idx ON club_memberships (club_id, status);

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

CREATE TYPE profile_verification_status AS ENUM ('pending', 'approved', 'rejected');

CREATE TABLE profiles (
    user_id         UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    club_id         UUID NOT NULL REFERENCES clubs (id),
    phone           TEXT,
    level           TEXT NOT NULL,
    slug            TEXT NOT NULL,
    gender          TEXT,
    city            TEXT NOT NULL DEFAULT 'Пушкин',
    bio             TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT,
    avatar_file_id  UUID,
    player_status   TEXT NOT NULL DEFAULT 'active',
    birth_date      DATE,
    verification_status profile_verification_status NOT NULL DEFAULT 'pending',
    verified_at     TIMESTAMPTZ,
    verified_by     UUID REFERENCES users (id) ON DELETE SET NULL,
    verification_note TEXT,
    rating          DOUBLE PRECISION NOT NULL DEFAULT 1200,
    points          INT NOT NULL DEFAULT 0,
    singles_points  INT NOT NULL DEFAULT 0,
    doubles_points  INT NOT NULL DEFAULT 0,
    matches_played  INT NOT NULL DEFAULT 0,
    wins            INT NOT NULL DEFAULT 0,
    losses          INT NOT NULL DEFAULT 0,
    notification_prefs JSONB NOT NULL DEFAULT '{
        "tournaments": true,
        "reminders": true,
        "results": true,
        "news": false
    }'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT profiles_level_chk CHECK (
        level IN ('masters', 'challenger', 'futures', 'rookies', 'beginners', 'newcomers')
    ),
    CONSTRAINT profiles_club_slug_uidx UNIQUE (club_id, slug),
    CONSTRAINT profiles_gender_chk CHECK (gender IS NULL OR gender IN ('male', 'female')),
    CONSTRAINT profiles_player_status_chk CHECK (player_status IN ('active', 'blocked', 'archived')),
    CONSTRAINT profiles_rating_chk CHECK (rating >= 800),
    CONSTRAINT profiles_points_chk CHECK (points >= 0),
    CONSTRAINT profiles_singles_points_chk CHECK (singles_points >= 0),
    CONSTRAINT profiles_doubles_points_chk CHECK (doubles_points >= 0),
    CONSTRAINT profiles_matches_played_chk CHECK (matches_played >= 0),
    CONSTRAINT profiles_wins_chk CHECK (wins >= 0),
    CONSTRAINT profiles_losses_chk CHECK (losses >= 0)
);

CREATE INDEX profiles_club_id_idx ON profiles (club_id);
CREATE INDEX profiles_rating_idx ON profiles (rating DESC);
CREATE INDEX profiles_points_idx ON profiles (points DESC);
CREATE INDEX profiles_level_idx ON profiles (level);
CREATE INDEX profiles_player_status_idx ON profiles (player_status);
CREATE INDEX profiles_verification_status_idx ON profiles (verification_status);

CREATE TYPE verification_kind AS ENUM ('email', 'reset', 'invite', 'sso');

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

CREATE TABLE user_consents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    version     TEXT NOT NULL,
    granted     BOOLEAN NOT NULL DEFAULT TRUE,
    ip          TEXT,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_consents_kind_chk CHECK (
        kind IN ('pdn_processing', 'public_profile', 'marketing', 'withdrawal')
    )
);

CREATE INDEX user_consents_user_id_idx ON user_consents (user_id, created_at DESC);
CREATE INDEX user_consents_kind_idx ON user_consents (kind, created_at DESC);

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
    'bye',
    'disqualified'
);

CREATE TYPE bracket_status AS ENUM (
    'none',
    'draft',
    'published'
);

CREATE TABLE tournaments (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id                 UUID NOT NULL REFERENCES clubs (id),
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
    cover_file_id           UUID,
    season                  INT NOT NULL,
    created_by              UUID REFERENCES users (id) ON DELETE SET NULL,
    bracket_status          bracket_status NOT NULL DEFAULT 'none',
    bracket_published_at    TIMESTAMPTZ,
    bracket_version         INT NOT NULL DEFAULT 0,
    bracket_third_place     BOOLEAN NOT NULL DEFAULT false,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tournaments_club_slug_uidx UNIQUE (club_id, slug),
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

CREATE INDEX tournaments_club_id_idx ON tournaments (club_id);
CREATE INDEX tournaments_club_status_idx ON tournaments (club_id, status);
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

CREATE TYPE rating_event_kind AS ENUM (
    'match',
    'tournament',
    'manual',
    'import',
    'recalc'
);

CREATE TABLE rating_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id         UUID NOT NULL REFERENCES clubs (id),
    season          INT NOT NULL,
    version         INT NOT NULL,
    coefficients    JSONB NOT NULL,
    active_from     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rating_configs_club_season_version_uidx UNIQUE (club_id, season, version),
    CONSTRAINT rating_configs_version_chk CHECK (version >= 1)
);

CREATE INDEX rating_configs_club_season_idx ON rating_configs (club_id, season, version DESC);
CREATE INDEX rating_configs_season_idx ON rating_configs (season, version DESC);

CREATE TABLE rating_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    match_id        UUID REFERENCES matches (id) ON DELETE SET NULL,
    tournament_id   UUID REFERENCES tournaments (id) ON DELETE SET NULL,
    kind            rating_event_kind NOT NULL,
    discipline      TEXT NOT NULL DEFAULT 'singles',
    points_delta    INT NOT NULL DEFAULT 0,
    rating_delta    DOUBLE PRECISION NOT NULL DEFAULT 0,
    rating_after    DOUBLE PRECISION NOT NULL,
    points_after    INT NOT NULL DEFAULT 0,
    config_id       UUID REFERENCES rating_configs (id) ON DELETE SET NULL,
    reason          TEXT NOT NULL DEFAULT '',
    created_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rating_events_rating_after_chk CHECK (rating_after >= 800),
    CONSTRAINT rating_events_points_after_chk CHECK (points_after >= 0),
    CONSTRAINT rating_events_discipline_chk CHECK (discipline IN ('singles', 'doubles'))
);

CREATE UNIQUE INDEX rating_events_match_player_kind_uidx
    ON rating_events (match_id, player_id, kind)
    WHERE match_id IS NOT NULL;

CREATE UNIQUE INDEX rating_events_tournament_player_kind_uidx
    ON rating_events (tournament_id, player_id, kind)
    WHERE tournament_id IS NOT NULL AND kind = 'tournament';

CREATE INDEX rating_events_player_id_idx ON rating_events (player_id, created_at DESC);
CREATE INDEX rating_events_created_at_idx ON rating_events (created_at DESC);
CREATE INDEX rating_events_discipline_idx ON rating_events (discipline)
    WHERE kind = 'tournament';

CREATE TABLE rankings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id             UUID NOT NULL REFERENCES clubs (id),
    season              INT NOT NULL,
    player_id           UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    position            INT NOT NULL,
    previous_position   INT,
    points              INT NOT NULL,
    rating              DOUBLE PRECISION NOT NULL,
    singles_points      INT NOT NULL DEFAULT 0,
    doubles_points      INT NOT NULL DEFAULT 0,
    category            TEXT NOT NULL,
    calculated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rankings_club_season_player_calc_uidx UNIQUE (club_id, season, player_id, calculated_at),
    CONSTRAINT rankings_position_chk CHECK (position >= 1),
    CONSTRAINT rankings_points_chk CHECK (points >= 0),
    CONSTRAINT rankings_category_chk CHECK (
        category IN ('masters', 'challenger', 'futures', 'rookies', 'beginners', 'newcomers')
    )
);

CREATE INDEX rankings_club_season_calc_idx ON rankings (club_id, season, calculated_at DESC);
CREATE INDEX rankings_club_season_position_idx ON rankings (club_id, season, calculated_at DESC, position);
CREATE INDEX rankings_season_calc_idx ON rankings (season, calculated_at DESC);
CREATE INDEX rankings_season_position_idx ON rankings (season, calculated_at DESC, position);

CREATE TYPE content_status AS ENUM ('draft', 'published', 'archived');

CREATE TYPE club_event_kind AS ENUM ('tournament', 'camp', 'school', 'club');

CREATE TABLE news_posts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id         UUID NOT NULL REFERENCES clubs (id),
    slug            TEXT NOT NULL,
    title           TEXT NOT NULL,
    excerpt         TEXT NOT NULL DEFAULT '',
    body_json       JSONB NOT NULL DEFAULT '[]'::jsonb,
    cover_url       TEXT,
    cover_file_id   UUID,
    tag             TEXT NOT NULL DEFAULT '',
    author_name     TEXT NOT NULL DEFAULT '',
    author_id       UUID REFERENCES users (id) ON DELETE SET NULL,
    status          content_status NOT NULL DEFAULT 'draft',
    published_at    TIMESTAMPTZ,
    reading_time    INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT news_posts_club_slug_uidx UNIQUE (club_id, slug),
    CONSTRAINT news_posts_title_chk CHECK (char_length(trim(title)) >= 3),
    CONSTRAINT news_posts_reading_time_chk CHECK (reading_time >= 1 AND reading_time <= 120)
);

CREATE INDEX news_posts_club_id_idx ON news_posts (club_id);
CREATE INDEX news_posts_club_status_published_idx ON news_posts (club_id, status, published_at DESC);
CREATE INDEX news_posts_status_published_idx ON news_posts (status, published_at DESC);
CREATE INDEX news_posts_tag_idx ON news_posts (tag);

CREATE TABLE club_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id         UUID NOT NULL REFERENCES clubs (id),
    slug            TEXT NOT NULL,
    title           TEXT NOT NULL,
    kind            club_event_kind NOT NULL DEFAULT 'club',
    description     TEXT NOT NULL DEFAULT '',
    starts_at       TIMESTAMPTZ NOT NULL,
    location        TEXT NOT NULL DEFAULT '',
    cover_url       TEXT,
    cover_file_id   UUID,
    seats_left      INT,
    status          content_status NOT NULL DEFAULT 'draft',
    created_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT club_events_club_slug_uidx UNIQUE (club_id, slug),
    CONSTRAINT club_events_title_chk CHECK (char_length(trim(title)) >= 3),
    CONSTRAINT club_events_seats_chk CHECK (seats_left IS NULL OR seats_left >= 0)
);

CREATE INDEX club_events_club_id_idx ON club_events (club_id);
CREATE INDEX club_events_club_status_starts_idx ON club_events (club_id, status, starts_at ASC);
CREATE INDEX club_events_status_starts_idx ON club_events (status, starts_at ASC);
CREATE INDEX club_events_kind_idx ON club_events (kind);

CREATE TABLE pages (
    club_id         UUID NOT NULL REFERENCES clubs (id),
    slug            TEXT NOT NULL,
    title           TEXT NOT NULL,
    body_markdown   TEXT NOT NULL DEFAULT '',
    updated_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (club_id, slug),
    CONSTRAINT pages_title_chk CHECK (char_length(trim(title)) >= 2),
    CONSTRAINT pages_slug_chk CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$')
);

CREATE INDEX pages_club_id_idx ON pages (club_id);

CREATE TABLE files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id         UUID NOT NULL REFERENCES clubs (id),
    bucket          TEXT NOT NULL DEFAULT 'uploads',
    path            TEXT NOT NULL,
    original_name   TEXT NOT NULL DEFAULT '',
    mime            TEXT NOT NULL,
    size            BIGINT NOT NULL,
    width           INT,
    height          INT,
    checksum        BYTEA NOT NULL,
    uploaded_by     UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT files_path_uidx UNIQUE (bucket, path),
    CONSTRAINT files_size_chk CHECK (size > 0 AND size <= 20971520),
    CONSTRAINT files_mime_chk CHECK (mime IN (
        'image/jpeg', 'image/png', 'image/webp', 'image/gif'
    ))
);

CREATE INDEX files_club_id_idx ON files (club_id);
CREATE INDEX files_uploaded_by_idx ON files (uploaded_by);
CREATE INDEX files_created_at_idx ON files (created_at DESC);
