-- +goose Up
-- +goose StatementBegin
CREATE TABLE files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
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

CREATE INDEX files_uploaded_by_idx ON files (uploaded_by);
CREATE INDEX files_created_at_idx ON files (created_at DESC);

ALTER TABLE tournaments
    ADD COLUMN cover_file_id UUID REFERENCES files (id) ON DELETE SET NULL;

ALTER TABLE news_posts
    ADD COLUMN cover_file_id UUID REFERENCES files (id) ON DELETE SET NULL;

ALTER TABLE club_events
    ADD COLUMN cover_file_id UUID REFERENCES files (id) ON DELETE SET NULL;

ALTER TABLE profiles
    ADD COLUMN avatar_file_id UUID REFERENCES files (id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE profiles DROP COLUMN IF EXISTS avatar_file_id;
ALTER TABLE club_events DROP COLUMN IF EXISTS cover_file_id;
ALTER TABLE news_posts DROP COLUMN IF EXISTS cover_file_id;
ALTER TABLE tournaments DROP COLUMN IF EXISTS cover_file_id;
DROP TABLE IF EXISTS files;
-- +goose StatementEnd
