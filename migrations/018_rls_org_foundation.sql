-- 018_rls_org_foundation.sql
-- Row-Level Security foundation for org-scoped tables.
--
-- Introduces Supabase-style RLS: SQL helpers read session vars set by the app
-- layer, policies gate row visibility by org membership. Policies use a
-- NULL-bypass so existing connections (which don't yet SET app.current_*)
-- continue to work unchanged. Enforcement kicks in only after the Go side is
-- wired to set the session vars per-transaction (Slice 2).
--
-- Rollout phases:
--   Phase 1 (this migration): Policies in place, NULL-bypass → no-op in prod
--   Phase 2 (Go changes):     App sets app.current_account_id per request
--   Phase 3 (future):          Remove NULL-bypass, add FORCE ROW LEVEL SECURITY
--
-- Idempotent: safe to re-run.

-- ── Helper schema & functions ─────────────────────────────────────────────
-- Helpers that query RLS-enabled tables MUST be SECURITY DEFINER, otherwise
-- evaluating a policy that calls them re-triggers RLS on the same table and
-- Postgres errors with "infinite recursion detected in policy".

CREATE SCHEMA IF NOT EXISTS app;

-- Grant USAGE so non-superuser app roles can call the helpers below.
-- Functions default to EXECUTE by PUBLIC; schema USAGE is the gate.
GRANT USAGE ON SCHEMA app TO PUBLIC;

CREATE OR REPLACE FUNCTION app.current_account_id()
RETURNS BIGINT
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.current_account_id', true), '')::BIGINT;
$$;

CREATE OR REPLACE FUNCTION app.current_org_id()
RETURNS BIGINT
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.current_org_id', true), '')::BIGINT;
$$;

-- True if current account is a member of the given org.
-- SECURITY DEFINER bypasses RLS of the caller so policies on other tables can
-- use this helper without causing recursion on org_members itself.
CREATE OR REPLACE FUNCTION app.is_org_member(target_org_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM identity.org_members m
        WHERE m.org_id = target_org_id
          AND m.account_id = app.current_account_id()
    );
$$;

-- True if current account is owner/admin of the given org.
CREATE OR REPLACE FUNCTION app.is_org_admin(target_org_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM identity.org_members m
        WHERE m.org_id = target_org_id
          AND m.account_id = app.current_account_id()
          AND m.role IN ('owner', 'admin')
    );
$$;

-- ── identity.org_members ──────────────────────────────────────────────────
-- Self-read only: a user sees their own membership rows. Listing other members
-- of a shared org goes through admin APIs (Go layer), not raw SELECT.

ALTER TABLE identity.org_members ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS org_members_self_read ON identity.org_members;
CREATE POLICY org_members_self_read ON identity.org_members
    FOR SELECT
    USING (
        app.current_account_id() IS NULL
        OR account_id = app.current_account_id()
    );

DROP POLICY IF EXISTS org_members_admin_write ON identity.org_members;
CREATE POLICY org_members_admin_write ON identity.org_members
    FOR ALL
    USING (
        app.current_account_id() IS NULL
        OR app.is_org_admin(org_id)
    );

-- ── identity.org_api_keys ─────────────────────────────────────────────────

ALTER TABLE identity.org_api_keys ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS org_api_keys_member_access ON identity.org_api_keys;
CREATE POLICY org_api_keys_member_access ON identity.org_api_keys
    FOR ALL
    USING (
        app.current_account_id() IS NULL
        OR app.is_org_member(org_id)
    );

-- ── billing.org_wallets ───────────────────────────────────────────────────

ALTER TABLE billing.org_wallets ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS org_wallets_member_read ON billing.org_wallets;
CREATE POLICY org_wallets_member_read ON billing.org_wallets
    FOR SELECT
    USING (
        app.current_account_id() IS NULL
        OR app.is_org_member(org_id)
    );

DROP POLICY IF EXISTS org_wallets_admin_write ON billing.org_wallets;
CREATE POLICY org_wallets_admin_write ON billing.org_wallets
    FOR ALL
    USING (
        app.current_account_id() IS NULL
        OR app.is_org_admin(org_id)
    );

-- ── Comments for discoverability ──────────────────────────────────────────

COMMENT ON FUNCTION app.current_account_id() IS
    'Returns session-scoped account id set by Go app layer. NULL if unset (bypass mode).';
COMMENT ON FUNCTION app.current_org_id() IS
    'Returns session-scoped org id set by Go app layer. NULL if unset.';
COMMENT ON FUNCTION app.is_org_member(BIGINT) IS
    'SECURITY DEFINER: true if current account is member of the given org.';
COMMENT ON FUNCTION app.is_org_admin(BIGINT) IS
    'SECURITY DEFINER: true if current account is owner/admin of the given org.';
