-- 024_account_purges.sql
-- Audit trail for GDPR-grade account purges (Phase 4 / Sprint 1A).
--
-- Every Purge attempt records:
--   - who initiated (admin Web → /admin/v1/accounts/:id/delete-request)
--   - who approved (boss APP scanner — confirmed via QR-delegate biometric)
--   - target account_id
--   - cascade outcome (purging | completed | failed) + error if any
--   - audit metadata captured at confirm time (ip, ua, geo, biometric kind)
--
-- The biometric_attestation column is currently NULL on every row because
-- Sprint 1A only ships the backend; the Lutu APP UI that captures the
-- attestation lands in Sprint 1B. Backfill is not required — older purge
-- rows with NULL attestation simply pre-date the APP shipping the field.
--
-- Idempotent: re-running this migration is safe (CREATE IF NOT EXISTS).

CREATE TABLE IF NOT EXISTS identity.account_purges (
    id                       BIGSERIAL    PRIMARY KEY,
    account_id               BIGINT       NOT NULL,
    initiated_by             BIGINT       NOT NULL,
    approved_by              BIGINT,
    -- Status of the cascade. 'purging' marks an in-flight attempt;
    -- 'completed' marks a successful end-to-end purge; 'failed' marks
    -- an attempt that aborted partway. Re-runs on a previously failed
    -- row are allowed and create a NEW row rather than overwrite —
    -- audit is append-only.
    status                   VARCHAR(16)  NOT NULL DEFAULT 'purging',
    -- Free-form error captured when status='failed'. Trimmed to 1 KB so
    -- a stack trace from a misbehaving downstream cannot bloat rows.
    error                    TEXT,
    -- Confirm-time metadata captured from the Gin request context on
    -- the APP confirm path. All optional — older clients (or a Phase
    -- 1A pre-Sprint 1B backend) leave them NULL.
    biometric_attestation    VARCHAR(64),
    ip                       INET,
    ua                       VARCHAR(256),
    geo                      VARCHAR(64),
    started_at               TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at             TIMESTAMPTZ,

    -- FK to accounts deferred — ON DELETE SET NULL keeps audit history
    -- legible even if the row is later hard-deleted by support tooling.
    -- (Default Purge flow uses soft-delete via status=Deleted, not a
    -- hard DELETE, so this branch should be rare.)
    CONSTRAINT fk_account_purges_account
        FOREIGN KEY (account_id) REFERENCES identity.accounts(id) ON DELETE SET NULL,
    CONSTRAINT fk_account_purges_initiator
        FOREIGN KEY (initiated_by) REFERENCES identity.accounts(id) ON DELETE SET NULL,
    CONSTRAINT fk_account_purges_approver
        FOREIGN KEY (approved_by) REFERENCES identity.accounts(id) ON DELETE SET NULL
);

-- Lookup index — operators routinely ask "show me purges for account X"
-- from the audit dashboard (Sprint 3 Track 3B).
CREATE INDEX IF NOT EXISTS idx_account_purges_account_id
    ON identity.account_purges (account_id, started_at DESC);

-- Time-range index for the dashboard's date filter.
CREATE INDEX IF NOT EXISTS idx_account_purges_started_at
    ON identity.account_purges (started_at DESC);

-- Concurrent-purge lock — at most one in-flight (status='purging') row
-- per account. A second concurrent attempt fails the INSERT with
-- duplicate-key, which the Purge code translates to 409 Conflict. Once
-- the cascade completes (status flips to 'completed' or 'failed'), the
-- partial index no longer covers the row, so a future re-attempt is
-- allowed (e.g. retry after a transient cascade failure).
CREATE UNIQUE INDEX IF NOT EXISTS idx_account_purges_one_in_flight
    ON identity.account_purges (account_id)
    WHERE status = 'purging';
