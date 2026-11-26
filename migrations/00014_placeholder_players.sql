-- +goose Up
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_lower_chk;
ALTER TABLE users
  ADD CONSTRAINT users_email_lower_chk CHECK (email IS NULL OR email = lower(email));

DROP INDEX IF EXISTS users_email_uidx;
CREATE UNIQUE INDEX users_email_uidx ON users (email) WHERE email IS NOT NULL;

-- +goose Down
-- Fails if any placeholder (NULL email) rows remain.
DROP INDEX IF EXISTS users_email_uidx;
CREATE UNIQUE INDEX users_email_uidx ON users (email);
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_lower_chk;
ALTER TABLE users
  ADD CONSTRAINT users_email_lower_chk CHECK (email = lower(email));
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
