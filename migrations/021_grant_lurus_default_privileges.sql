-- 021_grant_lurus_default_privileges.sql
-- Ensure the application role 'lurus' has table/sequence privileges on all
-- current and future objects in the billing and identity schemas.
--
-- Background: migrations 014 (wallet_pre_authorizations) and 017
-- (reconciliation_issues) created tables owned by postgres without an
-- accompanying GRANT, so the app errored with "permission denied for table
-- <name>" until a manual GRANT was run. This migration:
--   1. Grants CRUD on every existing billing/identity table.
--   2. Grants USAGE on every existing sequence (for BIGSERIAL inserts).
--   3. Sets DEFAULT PRIVILEGES so any future table/sequence created by
--      postgres in these schemas automatically gets the grants.
--
-- Idempotent: GRANT is idempotent, ALTER DEFAULT PRIVILEGES with identical
-- spec is a no-op.

-- 1. Existing tables
GRANT SELECT, INSERT, UPDATE, DELETE
    ON ALL TABLES IN SCHEMA billing TO lurus;
GRANT SELECT, INSERT, UPDATE, DELETE
    ON ALL TABLES IN SCHEMA identity TO lurus;

-- 2. Existing sequences
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA billing TO lurus;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identity TO lurus;

-- 3. Future tables/sequences created by postgres in these schemas
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA billing
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO lurus;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA identity
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO lurus;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA billing
    GRANT USAGE, SELECT ON SEQUENCES TO lurus;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA identity
    GRANT USAGE, SELECT ON SEQUENCES TO lurus;

-- 4. App-schema helpers for RLS (migration 018)
GRANT USAGE ON SCHEMA app TO lurus;
-- EXECUTE on app.* functions is already PUBLIC by default.
