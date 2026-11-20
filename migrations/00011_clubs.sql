-- +goose Up
-- +goose StatementBegin
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

-- Stable default tenant for existing single-club data.
INSERT INTO clubs (id, slug, name, city, settings)
VALUES (
    '11111111-1111-4111-8111-111111111111',
    'mr-pushkin',
    'Mr. Pushkin',
    'Пушкин',
    '{"timezone":"Europe/Moscow"}'::jsonb
);

-- tournaments
ALTER TABLE tournaments
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE tournaments SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE tournaments
    ALTER COLUMN club_id SET NOT NULL;
ALTER TABLE tournaments
    DROP CONSTRAINT tournaments_slug_uidx;
ALTER TABLE tournaments
    ADD CONSTRAINT tournaments_club_slug_uidx UNIQUE (club_id, slug);
CREATE INDEX tournaments_club_id_idx ON tournaments (club_id);
CREATE INDEX tournaments_club_status_idx ON tournaments (club_id, status);

-- profiles
ALTER TABLE profiles
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE profiles SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE profiles
    ALTER COLUMN club_id SET NOT NULL;
ALTER TABLE profiles
    DROP CONSTRAINT profiles_slug_uidx;
ALTER TABLE profiles
    ADD CONSTRAINT profiles_club_slug_uidx UNIQUE (club_id, slug);
CREATE INDEX profiles_club_id_idx ON profiles (club_id);

-- news_posts
ALTER TABLE news_posts
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE news_posts SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE news_posts
    ALTER COLUMN club_id SET NOT NULL;
ALTER TABLE news_posts
    DROP CONSTRAINT news_posts_slug_uidx;
ALTER TABLE news_posts
    ADD CONSTRAINT news_posts_club_slug_uidx UNIQUE (club_id, slug);
CREATE INDEX news_posts_club_id_idx ON news_posts (club_id);
CREATE INDEX news_posts_club_status_published_idx ON news_posts (club_id, status, published_at DESC);

-- club_events
ALTER TABLE club_events
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE club_events SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE club_events
    ALTER COLUMN club_id SET NOT NULL;
ALTER TABLE club_events
    DROP CONSTRAINT club_events_slug_uidx;
ALTER TABLE club_events
    ADD CONSTRAINT club_events_club_slug_uidx UNIQUE (club_id, slug);
CREATE INDEX club_events_club_id_idx ON club_events (club_id);
CREATE INDEX club_events_club_status_starts_idx ON club_events (club_id, status, starts_at ASC);

-- pages: composite PK (club_id, slug)
ALTER TABLE pages
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE pages SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE pages
    ALTER COLUMN club_id SET NOT NULL;
ALTER TABLE pages DROP CONSTRAINT pages_pkey;
ALTER TABLE pages ADD PRIMARY KEY (club_id, slug);
CREATE INDEX pages_club_id_idx ON pages (club_id);

-- files
ALTER TABLE files
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE files SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE files
    ALTER COLUMN club_id SET NOT NULL;
CREATE INDEX files_club_id_idx ON files (club_id);

-- rating_configs
ALTER TABLE rating_configs
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE rating_configs SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE rating_configs
    ALTER COLUMN club_id SET NOT NULL;
ALTER TABLE rating_configs
    DROP CONSTRAINT rating_configs_season_version_uidx;
ALTER TABLE rating_configs
    ADD CONSTRAINT rating_configs_club_season_version_uidx UNIQUE (club_id, season, version);
CREATE INDEX rating_configs_club_season_idx ON rating_configs (club_id, season, version DESC);

-- rankings
ALTER TABLE rankings
    ADD COLUMN club_id UUID REFERENCES clubs (id);
UPDATE rankings SET club_id = '11111111-1111-4111-8111-111111111111';
ALTER TABLE rankings
    ALTER COLUMN club_id SET NOT NULL;
ALTER TABLE rankings
    DROP CONSTRAINT rankings_season_player_calc_uidx;
ALTER TABLE rankings
    ADD CONSTRAINT rankings_club_season_player_calc_uidx UNIQUE (club_id, season, player_id, calculated_at);
CREATE INDEX rankings_club_season_calc_idx ON rankings (club_id, season, calculated_at DESC);
CREATE INDEX rankings_club_season_position_idx ON rankings (club_id, season, calculated_at DESC, position);

-- Backfill memberships from existing users (map global role → club role).
INSERT INTO club_memberships (club_id, user_id, role, status)
SELECT
    '11111111-1111-4111-8111-111111111111'::uuid,
    u.id,
    CASE u.role
        WHEN 'owner' THEN 'owner'::club_member_role
        WHEN 'organizer' THEN 'organizer'::club_member_role
        WHEN 'superadmin' THEN 'owner'::club_member_role
        ELSE 'player'::club_member_role
    END,
    'active'::club_member_status
FROM users u
ON CONFLICT (club_id, user_id) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE rankings DROP CONSTRAINT IF EXISTS rankings_club_season_player_calc_uidx;
DROP INDEX IF EXISTS rankings_club_season_position_idx;
DROP INDEX IF EXISTS rankings_club_season_calc_idx;
ALTER TABLE rankings DROP COLUMN IF EXISTS club_id;
ALTER TABLE rankings
    ADD CONSTRAINT rankings_season_player_calc_uidx UNIQUE (season, player_id, calculated_at);

ALTER TABLE rating_configs DROP CONSTRAINT IF EXISTS rating_configs_club_season_version_uidx;
DROP INDEX IF EXISTS rating_configs_club_season_idx;
ALTER TABLE rating_configs DROP COLUMN IF EXISTS club_id;
ALTER TABLE rating_configs
    ADD CONSTRAINT rating_configs_season_version_uidx UNIQUE (season, version);

DROP INDEX IF EXISTS files_club_id_idx;
ALTER TABLE files DROP COLUMN IF EXISTS club_id;

DROP INDEX IF EXISTS pages_club_id_idx;
ALTER TABLE pages DROP CONSTRAINT IF EXISTS pages_pkey;
ALTER TABLE pages DROP COLUMN IF EXISTS club_id;
ALTER TABLE pages ADD PRIMARY KEY (slug);

DROP INDEX IF EXISTS club_events_club_status_starts_idx;
DROP INDEX IF EXISTS club_events_club_id_idx;
ALTER TABLE club_events DROP CONSTRAINT IF EXISTS club_events_club_slug_uidx;
ALTER TABLE club_events DROP COLUMN IF EXISTS club_id;
ALTER TABLE club_events ADD CONSTRAINT club_events_slug_uidx UNIQUE (slug);

DROP INDEX IF EXISTS news_posts_club_status_published_idx;
DROP INDEX IF EXISTS news_posts_club_id_idx;
ALTER TABLE news_posts DROP CONSTRAINT IF EXISTS news_posts_club_slug_uidx;
ALTER TABLE news_posts DROP COLUMN IF EXISTS club_id;
ALTER TABLE news_posts ADD CONSTRAINT news_posts_slug_uidx UNIQUE (slug);

DROP INDEX IF EXISTS profiles_club_id_idx;
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_club_slug_uidx;
ALTER TABLE profiles DROP COLUMN IF EXISTS club_id;
ALTER TABLE profiles ADD CONSTRAINT profiles_slug_uidx UNIQUE (slug);

DROP INDEX IF EXISTS tournaments_club_status_idx;
DROP INDEX IF EXISTS tournaments_club_id_idx;
ALTER TABLE tournaments DROP CONSTRAINT IF EXISTS tournaments_club_slug_uidx;
ALTER TABLE tournaments DROP COLUMN IF EXISTS club_id;
ALTER TABLE tournaments ADD CONSTRAINT tournaments_slug_uidx UNIQUE (slug);

DROP TABLE IF EXISTS club_memberships;
DROP TABLE IF EXISTS clubs;
DROP TYPE IF EXISTS club_member_status;
DROP TYPE IF EXISTS club_member_role;
DROP TYPE IF EXISTS club_status;
-- +goose StatementEnd
