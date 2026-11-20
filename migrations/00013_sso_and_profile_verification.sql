-- +goose NO TRANSACTION
-- +goose Up
ALTER TYPE verification_kind ADD VALUE IF NOT EXISTS 'sso';

CREATE TYPE profile_verification_status AS ENUM ('pending', 'approved', 'rejected');

ALTER TABLE profiles
    ADD COLUMN IF NOT EXISTS birth_date DATE,
    ADD COLUMN IF NOT EXISTS verification_status profile_verification_status NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS verified_by UUID REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS verification_note TEXT;

CREATE INDEX IF NOT EXISTS profiles_verification_status_idx ON profiles (verification_status);

-- +goose Down
DROP INDEX IF EXISTS profiles_verification_status_idx;
ALTER TABLE profiles
    DROP COLUMN IF EXISTS verification_note,
    DROP COLUMN IF EXISTS verified_by,
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS verification_status,
    DROP COLUMN IF EXISTS birth_date;
DROP TYPE IF EXISTS profile_verification_status;
