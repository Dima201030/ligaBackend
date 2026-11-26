-- +goose Up
ALTER TABLE profiles
    ADD COLUMN IF NOT EXISTS notification_prefs JSONB NOT NULL DEFAULT '{
        "tournaments": true,
        "reminders": true,
        "results": true,
        "news": false
    }'::jsonb;

-- +goose Down
ALTER TABLE profiles DROP COLUMN IF EXISTS notification_prefs;
