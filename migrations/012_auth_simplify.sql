-- 012_auth_simplify.sql
-- Simplify auth: username+password registration, email/phone optional.

-- 1. Make email nullable (was NOT NULL).
ALTER TABLE identity.accounts ALTER COLUMN email DROP NOT NULL;

-- 2. Replace old unique index on email with a partial unique index
--    (only enforced when email is non-empty).
DROP INDEX IF EXISTS identity.idx_accounts_email;
ALTER TABLE identity.accounts DROP CONSTRAINT IF EXISTS uni_accounts_email;
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_email_unique
    ON identity.accounts (email) WHERE email IS NOT NULL AND email != '';

-- 3. Partial unique index on phone (non-empty only).
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_phone_unique
    ON identity.accounts (phone) WHERE phone IS NOT NULL AND phone != '';

-- 4. Case-insensitive unique index on username.
DROP INDEX IF EXISTS identity.idx_accounts_username;
ALTER TABLE identity.accounts DROP CONSTRAINT IF EXISTS uni_accounts_username;
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_username_lower_unique
    ON identity.accounts (lower(username)) WHERE username IS NOT NULL AND username != '';
