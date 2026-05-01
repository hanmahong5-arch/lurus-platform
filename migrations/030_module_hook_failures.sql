-- 030_module_hook_failures.sql
-- Dead-letter queue for module hook failures (P1-9 in docs/平台硬化清单.md).
--
-- Background:
--   The platform fires async hooks after lifecycle events (account_created,
--   plan_changed, checkin, referral_signup, reconciliation_issue). Subscribers
--   include mail (Stalwart mailbox provisioning), notification (welcome
--   in-app + email), and newapi_sync (NewAPI mirror user creation).
--
--   Before this migration, hook failures were `slog.Warn`-and-forget. A
--   permanently failing hook (e.g. Stalwart down for hours, NewAPI admin
--   token revoked, notification service rate-limited) would silently
--   degrade thousands of registrations with no operator visibility and no
--   path to recovery.
--
-- Contract:
--   - Each hook is now wrapped by module.Registry's runWithRetryAndDLQ
--     helper: 3 attempts with exponential backoff (200ms→400ms→800ms,
--     ±20% jitter) before a row lands here.
--   - One row per (event, hook_name, account_id) — the UNIQUE constraint
--     makes ingestion idempotent across reconciler ticks; a recurring
--     failure increments `attempts` and bumps `last_failed_at` rather than
--     spawning duplicate rows.
--   - Operators inspect via GET /admin/v1/onboarding-failures and replay
--     individual rows via POST /admin/v1/onboarding-failures/:id/replay.
--     Replay re-fetches the fresh account from the store before invoking
--     the named hook — handles account-was-deleted-since-failure cleanly.
--   - `replayed_at IS NULL` is "still broken"; a non-null value means the
--     latest replay attempt succeeded. Rows are kept (audit trail) rather
--     than deleted on replay.
--
-- Idempotent: re-running this migration is safe.

CREATE SCHEMA IF NOT EXISTS module;

GRANT USAGE ON SCHEMA module TO lurus;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA module TO lurus;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA module TO lurus;
ALTER DEFAULT PRIVILEGES IN SCHEMA module GRANT ALL ON TABLES TO lurus;
ALTER DEFAULT PRIVILEGES IN SCHEMA module GRANT ALL ON SEQUENCES TO lurus;

CREATE TABLE IF NOT EXISTS module.hook_failures (
    id              BIGSERIAL    PRIMARY KEY,
    event           VARCHAR(64)  NOT NULL,
    hook_name       VARCHAR(64)  NOT NULL,
    account_id      BIGINT,
    payload         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    error           TEXT         NOT NULL,
    attempts        INT          NOT NULL DEFAULT 1,
    first_failed_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_failed_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    replayed_at     TIMESTAMPTZ
);

-- Unique-by-tuple lets the upsert path (event, hook_name, account_id) merge
-- recurring failures into a single row with attempts++ semantics. NULL
-- account_id (non-account-scoped events) doesn't conflict via UNIQUE since
-- Postgres treats NULLs as distinct, so we use a partial unique index for
-- the common case + a fallback unique index for the NULL case.
CREATE UNIQUE INDEX IF NOT EXISTS uq_hook_failures_account
    ON module.hook_failures (event, hook_name, account_id)
    WHERE account_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_hook_failures_no_account
    ON module.hook_failures (event, hook_name)
    WHERE account_id IS NULL;

-- Operator triage index: pending failures, newest first.
CREATE INDEX IF NOT EXISTS idx_hook_failures_pending
    ON module.hook_failures (last_failed_at DESC)
    WHERE replayed_at IS NULL;
