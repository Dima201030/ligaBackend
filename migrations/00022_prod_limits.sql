-- +goose Up
ALTER TABLE clubs DROP CONSTRAINT IF EXISTS clubs_slug_chk;
ALTER TABLE clubs ADD CONSTRAINT clubs_slug_chk CHECK (
    slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'
    AND char_length(slug) BETWEEN 3 AND 40
);

ALTER TABLE clubs DROP CONSTRAINT IF EXISTS clubs_name_chk;
ALTER TABLE clubs ADD CONSTRAINT clubs_name_chk CHECK (
    char_length(trim(name)) BETWEEN 3 AND 80
);

ALTER TABLE files DROP CONSTRAINT IF EXISTS files_size_chk;
ALTER TABLE files ADD CONSTRAINT files_size_chk CHECK (size > 0 AND size <= 5242880);

-- +goose Down
ALTER TABLE clubs DROP CONSTRAINT IF EXISTS clubs_slug_chk;
ALTER TABLE clubs ADD CONSTRAINT clubs_slug_chk CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$');

ALTER TABLE clubs DROP CONSTRAINT IF EXISTS clubs_name_chk;
ALTER TABLE clubs ADD CONSTRAINT clubs_name_chk CHECK (char_length(trim(name)) >= 2);

ALTER TABLE files DROP CONSTRAINT IF EXISTS files_size_chk;
ALTER TABLE files ADD CONSTRAINT files_size_chk CHECK (size > 0 AND size <= 20971520);
