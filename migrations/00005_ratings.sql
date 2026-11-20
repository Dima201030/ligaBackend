-- +goose Up
-- +goose StatementBegin
ALTER TABLE profiles
    ADD COLUMN slug TEXT,
    ADD COLUMN gender TEXT,
    ADD COLUMN city TEXT NOT NULL DEFAULT 'Пушкин',
    ADD COLUMN bio TEXT NOT NULL DEFAULT '',
    ADD COLUMN avatar_url TEXT,
    ADD COLUMN player_status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN rating DOUBLE PRECISION NOT NULL DEFAULT 1200,
    ADD COLUMN points INT NOT NULL DEFAULT 0,
    ADD COLUMN matches_played INT NOT NULL DEFAULT 0,
    ADD COLUMN wins INT NOT NULL DEFAULT 0,
    ADD COLUMN losses INT NOT NULL DEFAULT 0;

UPDATE profiles p
SET slug = lower(regexp_replace(
        translate(
            u.last_name || '-' || u.first_name,
            'абвгдеёжзийклмнопрстуфхцчшщъыьэюяАБВГДЕЁЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ',
            'abvgdeejziyklmnoprstufhccss_y_euaABVGDEEJZIYKLMNOPRSTUFHCCSS_Y_EUA'
        ),
        '[^a-zA-Z0-9]+', '-', 'g'
    )) || '-' || substr(replace(u.id::text, '-', ''), 1, 8)
FROM users u
WHERE u.id = p.user_id
  AND (p.slug IS NULL OR p.slug = '');

ALTER TABLE profiles
    ALTER COLUMN slug SET NOT NULL,
    ADD CONSTRAINT profiles_slug_uidx UNIQUE (slug),
    ADD CONSTRAINT profiles_gender_chk CHECK (gender IS NULL OR gender IN ('male', 'female')),
    ADD CONSTRAINT profiles_player_status_chk CHECK (player_status IN ('active', 'blocked', 'archived')),
    ADD CONSTRAINT profiles_rating_chk CHECK (rating >= 800),
    ADD CONSTRAINT profiles_points_chk CHECK (points >= 0),
    ADD CONSTRAINT profiles_matches_played_chk CHECK (matches_played >= 0),
    ADD CONSTRAINT profiles_wins_chk CHECK (wins >= 0),
    ADD CONSTRAINT profiles_losses_chk CHECK (losses >= 0);

CREATE INDEX profiles_rating_idx ON profiles (rating DESC);
CREATE INDEX profiles_points_idx ON profiles (points DESC);
CREATE INDEX profiles_level_idx ON profiles (level);
CREATE INDEX profiles_player_status_idx ON profiles (player_status);

CREATE TYPE rating_event_kind AS ENUM (
    'match',
    'tournament',
    'manual',
    'import',
    'recalc'
);

CREATE TABLE rating_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    season          INT NOT NULL,
    version         INT NOT NULL,
    coefficients    JSONB NOT NULL,
    active_from     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rating_configs_season_version_uidx UNIQUE (season, version),
    CONSTRAINT rating_configs_version_chk CHECK (version >= 1)
);

CREATE INDEX rating_configs_season_idx ON rating_configs (season, version DESC);

CREATE TABLE rating_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    match_id        UUID REFERENCES matches (id) ON DELETE SET NULL,
    tournament_id   UUID REFERENCES tournaments (id) ON DELETE SET NULL,
    kind            rating_event_kind NOT NULL,
    points_delta    INT NOT NULL DEFAULT 0,
    rating_delta    DOUBLE PRECISION NOT NULL DEFAULT 0,
    rating_after    DOUBLE PRECISION NOT NULL,
    points_after    INT NOT NULL DEFAULT 0,
    config_id       UUID REFERENCES rating_configs (id) ON DELETE SET NULL,
    reason          TEXT NOT NULL DEFAULT '',
    created_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rating_events_rating_after_chk CHECK (rating_after >= 800),
    CONSTRAINT rating_events_points_after_chk CHECK (points_after >= 0)
);

CREATE UNIQUE INDEX rating_events_match_player_kind_uidx
    ON rating_events (match_id, player_id, kind)
    WHERE match_id IS NOT NULL;

CREATE UNIQUE INDEX rating_events_tournament_player_kind_uidx
    ON rating_events (tournament_id, player_id, kind)
    WHERE tournament_id IS NOT NULL AND kind = 'tournament';

CREATE INDEX rating_events_player_id_idx ON rating_events (player_id, created_at DESC);
CREATE INDEX rating_events_created_at_idx ON rating_events (created_at DESC);

CREATE TABLE rankings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
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
    CONSTRAINT rankings_season_player_calc_uidx UNIQUE (season, player_id, calculated_at),
    CONSTRAINT rankings_position_chk CHECK (position >= 1),
    CONSTRAINT rankings_points_chk CHECK (points >= 0),
    CONSTRAINT rankings_category_chk CHECK (
        category IN ('masters', 'challenger', 'futures', 'rookies', 'beginners', 'newcomers')
    )
);

CREATE INDEX rankings_season_calc_idx ON rankings (season, calculated_at DESC);
CREATE INDEX rankings_season_position_idx ON rankings (season, calculated_at DESC, position);

INSERT INTO rating_configs (season, version, coefficients)
VALUES (
    2026,
    1,
    '{
      "start_rating": 1200,
      "min_rating": 800,
      "window_weeks": 52,
      "best_singles": 10,
      "best_doubles": 5,
      "base_by_wins": {"0": 30, "1": 75, "2": 135, "3": 210, "4": 300, "5": 375, "6": 435},
      "category": {
        "masters": 1.6,
        "challenger": 1.3,
        "futures": 1.0,
        "rookies": 0.8,
        "beginners": 0.6,
        "newcomers": 0.4
      },
      "tournament_type": {
        "singles": 1.0,
        "doubles": 0.8,
        "mixed": 0.8,
        "cup": 1.4,
        "final": 1.5,
        "teams": 0.9
      },
      "elo_k": {
        "singles": {"lt10": 32, "lt30": 24, "default": 16},
        "doubles": {"lt10": 16, "lt30": 12, "default": 8}
      },
      "walkover_points_factor": 0.5,
      "walkover_elo": false,
      "consolation_factor": 0.5,
      "teams_share": 0.6
    }'::jsonb
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS rankings;
DROP TABLE IF EXISTS rating_events;
DROP TABLE IF EXISTS rating_configs;
DROP TYPE IF EXISTS rating_event_kind;

DROP INDEX IF EXISTS profiles_player_status_idx;
DROP INDEX IF EXISTS profiles_level_idx;
DROP INDEX IF EXISTS profiles_points_idx;
DROP INDEX IF EXISTS profiles_rating_idx;

ALTER TABLE profiles
    DROP CONSTRAINT IF EXISTS profiles_losses_chk,
    DROP CONSTRAINT IF EXISTS profiles_wins_chk,
    DROP CONSTRAINT IF EXISTS profiles_matches_played_chk,
    DROP CONSTRAINT IF EXISTS profiles_points_chk,
    DROP CONSTRAINT IF EXISTS profiles_rating_chk,
    DROP CONSTRAINT IF EXISTS profiles_player_status_chk,
    DROP CONSTRAINT IF EXISTS profiles_gender_chk,
    DROP CONSTRAINT IF EXISTS profiles_slug_uidx,
    DROP COLUMN IF EXISTS losses,
    DROP COLUMN IF EXISTS wins,
    DROP COLUMN IF EXISTS matches_played,
    DROP COLUMN IF EXISTS points,
    DROP COLUMN IF EXISTS rating,
    DROP COLUMN IF EXISTS player_status,
    DROP COLUMN IF EXISTS avatar_url,
    DROP COLUMN IF EXISTS bio,
    DROP COLUMN IF EXISTS city,
    DROP COLUMN IF EXISTS gender,
    DROP COLUMN IF EXISTS slug;
-- +goose StatementEnd
