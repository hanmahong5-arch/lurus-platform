-- 031_audit_events.sql
-- Persistent audit trail for destructive admin operations.
--
-- Background:
--   Until now the platform's destructive ops (QR delegate confirms,
--   refund approves, account purges, apps admin deletes /
--   clear-tombstone / reconcile-now) emitted slog.InfoContext lines that
--   went to stdout → kubectl logs → 1万行 ringbuffer. A pod restart
--   meant the audit trail evaporated. This migration introduces a
--   queryable Postgres table so "who did what when" survives restarts
--   and can be filtered by op, target, or time window.
--
-- Contract:
--   - One row per destructive event. Inserts are append-only — there
--     is no UPDATE path; corrections are a separate row.
--   - Rows are emitted best-effort by handlers: a Save failure logs at
--     WARN but does NOT fail the underlying op. Audit gaps are visible
--     to operators via the slog warn line + a missing row in the
--     dashboard.
--   - The `params` JSONB column carries op-specific context (app+env
--     for OIDC ops, refund_no for refund approvals, etc.) so the
--     schema does not need to grow a column per op. The `target_id` +
--     `target_kind` pair is the canonical (kind, id) handle the
--     audit dashboard groups by.
--   - `error` is populated only when result='failed'. The repo trims
--     it to 1024 chars before insert so a runaway stack trace from a
--     misbehaving downstream cannot bloat rows.
--
-- Idempotent: re-running this migration is safe.

CREATE SCHEMA IF NOT EXISTS module;

-- Schema-level grants from migration 030 already cover this table via
-- ALTER DEFAULT PRIVILEGES. We re-issue the explicit grant just to
-- shave race conditions during multi-step bootstraps where 030 ran
-- before lurus existed.
GRANT USAGE ON SCHEMA module TO lurus;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA module TO lurus;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA module TO lurus;
ALTER DEFAULT PRIVILEGES IN SCHEMA module GRANT ALL ON TABLES TO lurus;
ALTER DEFAULT PRIVILEGES IN SCHEMA module GRANT ALL ON SEQUENCES TO lurus;

CREATE TABLE IF NOT EXISTS module.audit_events (
    id           BIGSERIAL    PRIMARY KEY,
    op           VARCHAR(64)  NOT NULL,             -- e.g. "delete_oidc_app", "approve_refund"
    actor_id     BIGINT,                            -- the admin who triggered (NULL for cron/automation)
    target_id    BIGINT,                            -- account_id / refund_id / app_id (depending on op)
    target_kind  VARCHAR(32),                       -- "account" / "refund" / "oidc_app" / "hook_failure" / "account_purge"
    params       JSONB        NOT NULL DEFAULT '{}'::jsonb,  -- op-specific (app+env / refund_no / etc.)
    result       VARCHAR(16)  NOT NULL,             -- "success" / "failed"
    error        TEXT,                              -- non-null only when result='failed'; trimmed to 1024 chars
    ip           VARCHAR(64),
    user_agent   TEXT,
    occurred_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    request_id   VARCHAR(128)                       -- correlation with slog
);

-- Operator triage index: newest events first. Covers the default
-- "/admin/v1/audit-events" landing query (no filter, latest 50).
CREATE INDEX IF NOT EXISTS idx_audit_events_occurred
    ON module.audit_events (occurred_at DESC);

-- Per-op timeline: "show me the last 100 delete_oidc_app events".
CREATE INDEX IF NOT EXISTS idx_audit_events_op_time
    ON module.audit_events (op, occurred_at DESC);

-- Per-target lookup: "what destructive ops happened against this
-- account / refund?". Partial — rows with NULL target_id (e.g.
-- reconcile_now) don't bloat the index.
CREATE INDEX IF NOT EXISTS idx_audit_events_target
    ON module.audit_events (target_kind, target_id)
    WHERE target_id IS NOT NULL;
