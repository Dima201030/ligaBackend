-- +goose Up
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

-- +goose Down
DROP TABLE IF EXISTS user_consents;
