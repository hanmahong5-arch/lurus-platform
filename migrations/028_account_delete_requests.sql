-- 028_account_delete_requests.sql
-- User-self-initiated account deletion requests with cooling-off period.
--
-- Distinct from identity.account_purges (migration 024) — that table is the
-- audit trail for the admin-driven QR-delegate purge cascade, where the
-- cascade runs immediately after the boss biometric-confirms. This table
-- captures the *user-initiated* flow: the user taps "注销账号" in the Lutu
-- APP, submits a reason, and the request sits in 'pending' for a 30-day
-- cooling-off window during which they can cancel by simply logging in
-- again. Once the window elapses, a separate worker (out of scope for this
-- migration) will pick the row up and dispatch the same purge cascade
-- under a synthetic "approved by self" audit row.
--
-- PIPL §47 (right-to-delete) + GDPR Art. 17 compliance entry point.
--
-- Idempotent: re-running this migration is safe.

CREATE TABLE IF NOT EXISTS identity.account_delete_requests (
    id                    BIGSERIAL    PRIMARY KEY,
    account_id            BIGINT       NOT NULL,
    -- requested_by mirrors initiated_by on account_purges. For the self-
    -- service flow this equals account_id, but keeping the column means a
    -- future "请客服代为提交" path (CS rep on user's behalf) can populate a
    -- different value without a schema change.
    requested_by          BIGINT       NOT NULL,
    -- Status lifecycle:
    --   pending   → freshly submitted, sitting in the cooling-off window
    --   cancelled → user changed their mind (logged in again, or hit
    --               POST /api/v1/account/me/delete-request/cancel)
    --   completed → cooling-off elapsed AND the purge cascade succeeded
    --               (a row is also written to account_purges)
    --   expired   → cooling-off elapsed but the cascade has not yet run
    --               (terminal between the cron pickup and cascade success)
    status                VARCHAR(16)  NOT NULL DEFAULT 'pending',
    -- Reason taxonomy. Enforced at the app layer with a closed enum
    -- (no_longer_using | privacy_concern | experience_issue |
    -- found_alternative | other). Stored as VARCHAR so adding a new code
    -- later does not require an ALTER TYPE.
    reason                VARCHAR(32),
    -- Free-text user explanation. Truncated to 500 chars at the handler
    -- (we don't want to fail-blame a user mid-destructive-flow on a
    -- length nit). 1 KB hard cap here is belt-and-braces.
    reason_text           VARCHAR(1024),
    -- Cooling-off deadline. Computed as requested_at + 30 days at insert
    -- time and never mutated; the worker compares NOW() against this.
    cooling_off_until     TIMESTAMPTZ  NOT NULL,
    requested_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    cancelled_at          TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,

    CONSTRAINT fk_account_delete_requests_account
        FOREIGN KEY (account_id) REFERENCES identity.accounts(id) ON DELETE SET NULL,
    CONSTRAINT fk_account_delete_requests_requester
        FOREIGN KEY (requested_by) REFERENCES identity.accounts(id) ON DELETE SET NULL
);

-- Latest-request lookup (status filtered) — the handler queries this on
-- every POST /api/v1/account/me/delete-request to enforce idempotency.
CREATE INDEX IF NOT EXISTS idx_account_delete_requests_account_id
    ON identity.account_delete_requests (account_id, requested_at DESC);

-- Worker scan index — orders by deadline so the oldest expired pending
-- request is picked first.
CREATE INDEX IF NOT EXISTS idx_account_delete_requests_cooling_off
    ON identity.account_delete_requests (cooling_off_until)
    WHERE status = 'pending';

-- Idempotency lock: at most one pending self-delete request per account.
-- A second POST while pending hits the partial UNIQUE and the handler
-- translates it back to a 200 idempotent response carrying the existing
-- request id (matches the admin endpoint's already-deleted handling shape).
CREATE UNIQUE INDEX IF NOT EXISTS idx_account_delete_requests_one_pending
    ON identity.account_delete_requests (account_id)
    WHERE status = 'pending';
