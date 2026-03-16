-- Migration 001: identity schema
-- Unified account and product catalog tables.

CREATE SCHEMA IF NOT EXISTS identity;

-- Unified accounts: each user's "Lurus ID"
CREATE TABLE IF NOT EXISTS identity.accounts (
    id              BIGSERIAL PRIMARY KEY,
    lurus_id        VARCHAR(16) UNIQUE NOT NULL,
    zitadel_sub     VARCHAR(128) UNIQUE,
    display_name    VARCHAR(64) NOT NULL,
    avatar_url      TEXT,
    email           VARCHAR(255) UNIQUE NOT NULL,
    email_verified  BOOLEAN DEFAULT false,
    phone           VARCHAR(32),
    phone_verified  BOOLEAN DEFAULT false,
    status          SMALLINT DEFAULT 1,           -- 1=active 2=suspended 3=deleted
    locale          VARCHAR(8) DEFAULT 'zh-CN',
    referrer_id     BIGINT REFERENCES identity.accounts(id),
    aff_code        VARCHAR(32) UNIQUE NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_accounts_email ON identity.accounts(email);
CREATE INDEX IF NOT EXISTS idx_accounts_zitadel_sub ON identity.accounts(zitadel_sub);
CREATE INDEX IF NOT EXISTS idx_accounts_referrer ON identity.accounts(referrer_id);

-- OAuth provider bindings (github/discord/wechat/telegram/linuxdo/oidc)
CREATE TABLE IF NOT EXISTS identity.account_oauth_bindings (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES identity.accounts(id) ON DELETE CASCADE,
    provider        VARCHAR(32) NOT NULL,
    provider_id     VARCHAR(128) NOT NULL,
    provider_email  VARCHAR(255),
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(provider, provider_id)
);

CREATE INDEX IF NOT EXISTS idx_oauth_account ON identity.account_oauth_bindings(account_id);

-- Product catalog (extensible via admin API — no code changes needed)
CREATE TABLE IF NOT EXISTS identity.products (
    id              VARCHAR(32) PRIMARY KEY,
    name            VARCHAR(64) NOT NULL,
    description     TEXT,
    category        VARCHAR(32),
    billing_model   VARCHAR(32) NOT NULL,
    status          SMALLINT DEFAULT 1,
    sort_order      INT DEFAULT 0,
    config          JSONB DEFAULT '{}'
);

-- Product plans (features stored as JSONB for zero-schema-migration extensibility)
CREATE TABLE IF NOT EXISTS identity.product_plans (
    id              BIGSERIAL PRIMARY KEY,
    product_id      VARCHAR(32) NOT NULL REFERENCES identity.products(id),
    code            VARCHAR(32) NOT NULL,
    name            VARCHAR(64) NOT NULL,
    billing_cycle   VARCHAR(16),    -- forever/weekly/monthly/quarterly/yearly/one_time
    price_cny       DECIMAL(10,2) DEFAULT 0,
    price_usd       DECIMAL(10,2) DEFAULT 0,
    is_default      BOOLEAN DEFAULT false,
    sort_order      INT DEFAULT 0,
    status          SMALLINT DEFAULT 1,
    features        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(product_id, code)
);

CREATE INDEX IF NOT EXISTS idx_plans_product ON identity.product_plans(product_id);

-- Subscriptions (one live subscription per account per product)
CREATE TABLE IF NOT EXISTS identity.subscriptions (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES identity.accounts(id),
    product_id      VARCHAR(32) NOT NULL REFERENCES identity.products(id),
    plan_id         BIGINT NOT NULL REFERENCES identity.product_plans(id),
    status          VARCHAR(16) DEFAULT 'pending',
    started_at      TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    grace_until     TIMESTAMPTZ,
    auto_renew      BOOLEAN DEFAULT false,
    payment_method  VARCHAR(32),
    external_sub_id VARCHAR(128),
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Only one live subscription per account+product
CREATE UNIQUE INDEX IF NOT EXISTS idx_subs_active_unique
    ON identity.subscriptions(account_id, product_id)
    WHERE status IN ('active','grace','trial');

CREATE INDEX IF NOT EXISTS idx_subs_account ON identity.subscriptions(account_id);
CREATE INDEX IF NOT EXISTS idx_subs_product ON identity.subscriptions(product_id);
CREATE INDEX IF NOT EXISTS idx_subs_expires ON identity.subscriptions(expires_at)
    WHERE expires_at IS NOT NULL AND status = 'active';

-- Entitlement snapshot: the single source of truth consumed by all products
CREATE TABLE IF NOT EXISTS identity.account_entitlements (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES identity.accounts(id),
    product_id      VARCHAR(32) NOT NULL REFERENCES identity.products(id),
    key             VARCHAR(64) NOT NULL,
    value           TEXT NOT NULL,
    value_type      VARCHAR(16) DEFAULT 'string',
    source          VARCHAR(32),
    source_ref      VARCHAR(128),
    expires_at      TIMESTAMPTZ,
    UNIQUE(account_id, product_id, key)
);

CREATE INDEX IF NOT EXISTS idx_ents_account_product ON identity.account_entitlements(account_id, product_id);

-- VIP levels per account
CREATE TABLE IF NOT EXISTS identity.account_vip (
    account_id        BIGINT PRIMARY KEY REFERENCES identity.accounts(id),
    level             SMALLINT DEFAULT 0,
    level_name        VARCHAR(32),
    points            BIGINT DEFAULT 0,
    yearly_sub_grant  SMALLINT DEFAULT 0,
    spend_grant       SMALLINT DEFAULT 0,
    level_expires_at  TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ DEFAULT NOW()
);

-- Operator-configurable VIP level thresholds
CREATE TABLE IF NOT EXISTS identity.vip_level_configs (
    level                SMALLINT PRIMARY KEY,
    name                 VARCHAR(32) NOT NULL,
    min_spend_cny        DECIMAL(10,2) DEFAULT 0,
    yearly_sub_min_plan  VARCHAR(32),
    global_discount      DECIMAL(4,3) DEFAULT 1.000,
    perks_json           JSONB DEFAULT '{}'
);
