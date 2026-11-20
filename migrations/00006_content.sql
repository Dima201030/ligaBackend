-- +goose Up
-- +goose StatementBegin
CREATE TYPE content_status AS ENUM ('draft', 'published', 'archived');

CREATE TYPE club_event_kind AS ENUM ('tournament', 'camp', 'school', 'club');

CREATE TABLE news_posts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT NOT NULL,
    title           TEXT NOT NULL,
    excerpt         TEXT NOT NULL DEFAULT '',
    body_json       JSONB NOT NULL DEFAULT '[]'::jsonb,
    cover_url       TEXT,
    tag             TEXT NOT NULL DEFAULT '',
    author_name     TEXT NOT NULL DEFAULT '',
    author_id       UUID REFERENCES users (id) ON DELETE SET NULL,
    status          content_status NOT NULL DEFAULT 'draft',
    published_at    TIMESTAMPTZ,
    reading_time    INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT news_posts_slug_uidx UNIQUE (slug),
    CONSTRAINT news_posts_title_chk CHECK (char_length(trim(title)) >= 3),
    CONSTRAINT news_posts_reading_time_chk CHECK (reading_time >= 1 AND reading_time <= 120)
);

CREATE INDEX news_posts_status_published_idx ON news_posts (status, published_at DESC);
CREATE INDEX news_posts_tag_idx ON news_posts (tag);

CREATE TABLE club_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT NOT NULL,
    title           TEXT NOT NULL,
    kind            club_event_kind NOT NULL DEFAULT 'club',
    description     TEXT NOT NULL DEFAULT '',
    starts_at       TIMESTAMPTZ NOT NULL,
    location        TEXT NOT NULL DEFAULT '',
    cover_url       TEXT,
    seats_left      INT,
    status          content_status NOT NULL DEFAULT 'draft',
    created_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT club_events_slug_uidx UNIQUE (slug),
    CONSTRAINT club_events_title_chk CHECK (char_length(trim(title)) >= 3),
    CONSTRAINT club_events_seats_chk CHECK (seats_left IS NULL OR seats_left >= 0)
);

CREATE INDEX club_events_status_starts_idx ON club_events (status, starts_at ASC);
CREATE INDEX club_events_kind_idx ON club_events (kind);

CREATE TABLE pages (
    slug            TEXT PRIMARY KEY,
    title           TEXT NOT NULL,
    body_markdown   TEXT NOT NULL DEFAULT '',
    updated_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pages_title_chk CHECK (char_length(trim(title)) >= 2),
    CONSTRAINT pages_slug_chk CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$')
);

INSERT INTO pages (slug, title, body_markdown) VALUES
    ('club', 'Клуб', 'Клуб Mr. Pushkin — любительская теннисная лига в Пушкине.'),
    ('contacts', 'Контакты', 'Телефон и адрес клуба.'),
    ('rules', 'Правила', 'Регламент участия в турнирах лиги.');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS pages;
DROP TABLE IF EXISTS club_events;
DROP TABLE IF EXISTS news_posts;
DROP TYPE IF EXISTS club_event_kind;
DROP TYPE IF EXISTS content_status;
-- +goose StatementEnd
