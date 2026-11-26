-- +goose Up
-- +goose StatementBegin
ALTER TABLE tournaments
    ADD COLUMN is_hidden BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX tournaments_hidden_idx ON tournaments (club_id, is_hidden);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS tournaments_hidden_idx;
ALTER TABLE tournaments DROP COLUMN IF EXISTS is_hidden;
-- +goose StatementEnd
