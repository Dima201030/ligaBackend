-- +goose Up
-- +goose NO TRANSACTION
ALTER TYPE match_status ADD VALUE IF NOT EXISTS 'disqualified';

-- +goose Down
-- +goose NO TRANSACTION
-- Enum values cannot be removed safely in Postgres; left in place.
SELECT 1;
