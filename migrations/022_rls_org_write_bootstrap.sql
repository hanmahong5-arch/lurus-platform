-- 022_rls_org_write_bootstrap.sql
-- Resolve the bootstrap problem for RLS on write operations:
--   OrganizationService.Create must add the owner as the first member, but
--   the user isn't yet an admin, so the 018 write policy (is_org_admin)
--   would block the INSERT.
--
-- Solution: allow account_id = current_account_id() to INSERT themselves as
-- 'owner' iff identity.organizations.owner_account_id matches. After the
-- bootstrap INSERT, the user is now a member and subsequent operations flow
-- through is_org_admin() as before.
--
-- Same policy also naturally handles:
--   - Removed owner rejoining their own org (still the registered owner)
--   - Prevents non-owners from bypassing admin gating
--
-- Scope: updates org_members_admin_write; also widens org_api_keys and
-- org_wallets write policies so admins of their own org can mutate without
-- tripping NULL-bypass dependency.
--
-- Idempotent: DROP IF EXISTS + CREATE.

-- ── identity.org_members ──────────────────────────────────────────────────

DROP POLICY IF EXISTS org_members_admin_write ON identity.org_members;

CREATE POLICY org_members_admin_write ON identity.org_members
    FOR ALL
    USING (
        app.current_account_id() IS NULL
        OR app.is_org_admin(org_id)
    )
    WITH CHECK (
        app.current_account_id() IS NULL
        OR app.is_org_admin(org_id)
        -- Bootstrap: registered org owner can add themselves as 'owner'
        OR (
            account_id = app.current_account_id()
            AND role = 'owner'
            AND EXISTS (
                SELECT 1 FROM identity.organizations o
                WHERE o.id = org_id
                  AND o.owner_account_id = app.current_account_id()
            )
        )
    );

-- ── identity.org_api_keys ─────────────────────────────────────────────────
-- Keep existing "member access" semantics; no bootstrap case applies
-- (API keys can only be created after the org has members).

-- ── billing.org_wallets ───────────────────────────────────────────────────
-- Keep existing admin_write policy; wallets are managed by admin flows,
-- no bootstrap case.

-- (No changes to org_api_keys / org_wallets policies — they work as-is
-- once the caller is a member, which the bootstrap flow ensures for
-- org_members first.)
