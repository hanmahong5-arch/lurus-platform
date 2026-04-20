-- 019_rls_org_members_broaden_read.sql
-- Broaden the org_members SELECT policy.
--
-- The original policy in 018 restricted reads to "only your own membership
-- rows" (account_id = current_account_id()). This is too narrow for legitimate
-- access patterns like "check if target user is in org Y before removing
-- them" (OrganizationService.RemoveMember) or "list members of an org I
-- belong to" (admin UI). Widen to "see all members of orgs I am in", using
-- the SECURITY DEFINER helper app.is_org_member() to avoid policy recursion.
--
-- Non-breaking: policy still falls back to NULL-bypass when no session var set.
-- Idempotent: DROP-IF-EXISTS + CREATE.

DROP POLICY IF EXISTS org_members_self_read ON identity.org_members;
DROP POLICY IF EXISTS org_members_org_read ON identity.org_members;

CREATE POLICY org_members_org_read ON identity.org_members
    FOR SELECT
    USING (
        app.current_account_id() IS NULL
        OR app.is_org_member(org_id)
    );
