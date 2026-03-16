-- Migration 007: organization accounts for SaaS multi-tenant billing
-- Enables enterprise customers to create org accounts, add members, share token
-- balance, and authenticate via org-scoped API keys.

-- identity.organizations
CREATE TABLE IF NOT EXISTS identity.organizations (
    id               BIGSERIAL    PRIMARY KEY,
    name             TEXT         NOT NULL,
    slug             TEXT         NOT NULL UNIQUE,          -- URL-safe unique identifier
    owner_account_id BIGINT       NOT NULL REFERENCES identity.accounts(id),
    status           TEXT         NOT NULL DEFAULT 'active', -- active | suspended
    plan             TEXT         NOT NULL DEFAULT 'free',   -- free | team | enterprise
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- identity.org_members
CREATE TABLE IF NOT EXISTS identity.org_members (
    org_id     BIGINT NOT NULL REFERENCES identity.organizations(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES identity.accounts(id) ON DELETE CASCADE,
    role       TEXT   NOT NULL DEFAULT 'member',            -- owner | admin | member
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_org_members_account ON identity.org_members(account_id);

-- identity.org_api_keys
CREATE TABLE IF NOT EXISTS identity.org_api_keys (
    id           BIGSERIAL    PRIMARY KEY,
    org_id       BIGINT       NOT NULL REFERENCES identity.organizations(id) ON DELETE CASCADE,
    key_hash     TEXT         NOT NULL UNIQUE,   -- SHA-256 hex of raw key
    key_prefix   TEXT         NOT NULL,          -- first 8 chars for display only
    name         TEXT         NOT NULL,
    created_by   BIGINT       NOT NULL REFERENCES identity.accounts(id),
    last_used_at TIMESTAMPTZ,
    status       TEXT         NOT NULL DEFAULT 'active',   -- active | revoked
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_org_api_keys_org ON identity.org_api_keys(org_id);

-- billing.org_wallets (mirrors billing.wallets structure)
CREATE TABLE IF NOT EXISTS billing.org_wallets (
    org_id          BIGINT         PRIMARY KEY REFERENCES identity.organizations(id) ON DELETE CASCADE,
    balance         DECIMAL(14,4)  NOT NULL DEFAULT 0,
    frozen          DECIMAL(14,4)  NOT NULL DEFAULT 0,
    lifetime_topup  DECIMAL(14,4)  NOT NULL DEFAULT 0,
    lifetime_spend  DECIMAL(14,4)  NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now()
);
