-- Migration 015: Service API Keys with scoped permissions
-- Replaces the single INTERNAL_API_KEY with per-service keys that have
-- specific scopes, rate limits, and audit trails.

CREATE TABLE IF NOT EXISTS identity.service_api_keys (
    id              BIGSERIAL PRIMARY KEY,
    key_hash        VARCHAR(64)  UNIQUE NOT NULL,  -- SHA-256 of the raw key
    key_prefix      VARCHAR(12)  NOT NULL,          -- first 8 chars for identification in logs
    service_name    VARCHAR(64)  NOT NULL,          -- e.g. "lurus-api", "lucrum", "forge"
    description     TEXT,
    scopes          TEXT[]       NOT NULL DEFAULT '{}',  -- e.g. {"account:read","wallet:debit"}
    rate_limit_rpm  INT          NOT NULL DEFAULT 1000,  -- requests per minute
    status          SMALLINT     NOT NULL DEFAULT 1,     -- 1=active, 2=suspended, 3=revoked
    created_by      VARCHAR(64),
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_svc_api_keys_service ON identity.service_api_keys (service_name);

COMMENT ON TABLE identity.service_api_keys IS
    'Per-service API keys with scoped permissions. Replaces the single shared INTERNAL_API_KEY.';
COMMENT ON COLUMN identity.service_api_keys.scopes IS
    'Allowed scopes: account:read, account:write, wallet:read, wallet:debit, wallet:credit, entitlement, checkout';
COMMENT ON COLUMN identity.service_api_keys.rate_limit_rpm IS
    'Maximum requests per minute for this service key. 0 = unlimited.';

-- Seed: migrate the existing INTERNAL_API_KEY as a legacy key with full access.
-- After deploying, create per-service keys via admin API and rotate.
-- The legacy key check remains as fallback (handled in code).
