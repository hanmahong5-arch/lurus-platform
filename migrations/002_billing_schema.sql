-- Migration 002: billing schema
-- Wallet, transactions, payment orders, redemption codes, referral rewards.

CREATE SCHEMA IF NOT EXISTS billing;

-- Unified wallet (shared across all Lurus products, 1 Credit = 1 CNY)
CREATE TABLE IF NOT EXISTS billing.wallets (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT UNIQUE NOT NULL,
    balance         DECIMAL(14,4) DEFAULT 0,
    frozen          DECIMAL(14,4) DEFAULT 0,
    lifetime_topup  DECIMAL(12,2) DEFAULT 0,
    lifetime_spend  DECIMAL(12,2) DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_wallets_account ON billing.wallets(account_id);

-- Immutable append-only ledger (never UPDATE, only INSERT)
CREATE TABLE IF NOT EXISTS billing.wallet_transactions (
    id              BIGSERIAL PRIMARY KEY,
    wallet_id       BIGINT NOT NULL,
    account_id      BIGINT NOT NULL,
    type            VARCHAR(32) NOT NULL,
    amount          DECIMAL(14,4) NOT NULL,
    balance_after   DECIMAL(14,4) NOT NULL,
    product_id      VARCHAR(32),
    reference_type  VARCHAR(32),
    reference_id    VARCHAR(128),
    description     TEXT,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wtx_account ON billing.wallet_transactions(account_id);
CREATE INDEX IF NOT EXISTS idx_wtx_wallet  ON billing.wallet_transactions(wallet_id);
CREATE INDEX IF NOT EXISTS idx_wtx_created ON billing.wallet_transactions(created_at DESC);

-- External payment orders
CREATE TABLE IF NOT EXISTS billing.payment_orders (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL,
    order_no        VARCHAR(64) UNIQUE NOT NULL,
    order_type      VARCHAR(32) NOT NULL,    -- topup/subscription/one_time
    product_id      VARCHAR(32),
    plan_id         BIGINT,
    amount_cny      DECIMAL(10,2) NOT NULL,
    currency        VARCHAR(8) DEFAULT 'CNY',
    payment_method  VARCHAR(32),
    status          VARCHAR(16) DEFAULT 'pending',
    external_id     VARCHAR(128),
    paid_at         TIMESTAMPTZ,
    callback_data   JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_account   ON billing.payment_orders(account_id);
CREATE INDEX IF NOT EXISTS idx_orders_status    ON billing.payment_orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_external  ON billing.payment_orders(external_id);

-- Redemption / promo codes
CREATE TABLE IF NOT EXISTS billing.redemption_codes (
    id              BIGSERIAL PRIMARY KEY,
    code            VARCHAR(32) UNIQUE NOT NULL,
    product_id      VARCHAR(32),
    reward_type     VARCHAR(32) NOT NULL,   -- credits/subscription_trial/quota_grant
    reward_value    DECIMAL(12,4),
    reward_metadata JSONB DEFAULT '{}',
    max_uses        INT DEFAULT 1,
    used_count      INT DEFAULT 0,
    expires_at      TIMESTAMPTZ,
    batch_id        VARCHAR(64),
    created_by      BIGINT,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Referral reward event log
CREATE TABLE IF NOT EXISTS billing.referral_reward_events (
    id              BIGSERIAL PRIMARY KEY,
    referrer_id     BIGINT NOT NULL,
    referee_id      BIGINT NOT NULL,
    event_type      VARCHAR(32),
    reward_credits  DECIMAL(10,4) NOT NULL,
    status          VARCHAR(16),
    triggered_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_referral_referrer ON billing.referral_reward_events(referrer_id);
CREATE INDEX IF NOT EXISTS idx_referral_referee  ON billing.referral_reward_events(referee_id);
