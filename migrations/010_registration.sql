-- 010_registration.sql
-- Adds username column to accounts for user-chosen usernames.

ALTER TABLE identity.accounts
    ADD COLUMN IF NOT EXISTS username VARCHAR(64);

-- Partial unique index: only enforce uniqueness when username is set.
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_username
    ON identity.accounts(username) WHERE username IS NOT NULL;
