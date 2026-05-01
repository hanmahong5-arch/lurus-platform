-- 029_org_kova_services.sql
-- Per-organization provisioned service registry — closes the F2 H-severity gap
-- from doc/audit-2026-04-30.md (the "platform → kova provisioning bridge").
--
-- One row per (org_id, service) pair. Today the only `service` produced is
-- 'kova' (a kova-rest tester instance on R6); the table is generic on purpose
-- so future provisioning targets (forge workspace, lucrum live-trade slot,
-- etc.) reuse the same shape without another DDL round.
--
-- Lifecycle:
--   pending     → POST /internal/v1/orgs/{id}/services/kova-tester accepted
--                 the request, R6 provision RPC is in flight or queued.
--   active      → R6 returned base_url + admin key; key SHA-256 stored, raw
--                 key returned to caller exactly once and never persisted.
--   failed      → R6 RPC errored after retries; metadata.error carries the
--                 reason. Callers may retry by re-POSTing.
--
-- The raw admin key is intentionally NOT stored. We store key_prefix (first 8
-- chars, useful for log correlation and humans saying "the key starting with
-- sk-kova-a1b2…") and key_hash (SHA-256, lets us recognise a leaked key in
-- audit logs without holding plaintext). Customers retrieve the raw key only
-- in the synchronous POST response; if they lose it they must rotate.
--
-- Idempotent: re-running this migration is safe.

CREATE TABLE IF NOT EXISTS billing.org_services (
    org_id           BIGINT       NOT NULL,
    service          VARCHAR(32)  NOT NULL,
    status           VARCHAR(16)  NOT NULL DEFAULT 'pending',
    base_url         TEXT,
    key_hash         VARCHAR(64),                  -- SHA-256 hex of raw admin key
    key_prefix       VARCHAR(16),                  -- first ~8 chars for log triage
    tester_name      VARCHAR(64),                  -- R6-side tester slug (= slug of org)
    port             INTEGER,                      -- kova-rest port on R6 (3010 + idx)
    metadata         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    provisioned_at   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    PRIMARY KEY (org_id, service),
    CONSTRAINT fk_org_services_org
        FOREIGN KEY (org_id) REFERENCES identity.organizations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_org_services_status
    ON billing.org_services (service, status);

-- Usage events from per-tenant kova workers. Each completed agent run reports
-- token + cost numbers here so billing can later roll them into invoices /
-- subscription overage. Append-only; no UPDATE path.
--
-- cost_micros uses 1e-6 USD units (six decimal places of precision) so we can
-- hold both per-token markup and bulk monthly totals in BIGINT without
-- floating-point drift.
CREATE TABLE IF NOT EXISTS billing.usage_events (
    id              BIGSERIAL    PRIMARY KEY,
    org_id          BIGINT       NOT NULL,
    service         VARCHAR(32)  NOT NULL,
    tester_name     VARCHAR(64),
    agent_id        VARCHAR(128),
    tokens_in       BIGINT       NOT NULL DEFAULT 0,
    tokens_out      BIGINT       NOT NULL DEFAULT 0,
    cost_micros     BIGINT       NOT NULL DEFAULT 0,
    occurred_at     TIMESTAMPTZ  NOT NULL,
    received_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    metadata        JSONB        NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT fk_usage_events_org
        FOREIGN KEY (org_id) REFERENCES identity.organizations(id) ON DELETE CASCADE
);

-- Roll-up scan index for billing aggregation: "all events for org X between
-- dates Y and Z" is the dominant query pattern.
CREATE INDEX IF NOT EXISTS idx_usage_events_org_occurred
    ON billing.usage_events (org_id, service, occurred_at DESC);

-- Operator triage index: "show me everything kova reported in the last hour"
-- without scanning the org-keyed index.
CREATE INDEX IF NOT EXISTS idx_usage_events_received
    ON billing.usage_events (received_at DESC);
