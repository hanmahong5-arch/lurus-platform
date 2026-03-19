-- 014_checkout_preauth.sql
-- Adds checkout enhancements to payment_orders (source tracking, idempotency, expiry, pay URL)
-- and creates wallet_pre_authorizations table for streaming API pre-auth/settle flow.
-- Idempotent: IF NOT EXISTS / ADD COLUMN IF NOT EXISTS.

-- 1. Enhance payment_orders for cross-service checkout
ALTER TABLE billing.payment_orders
    ADD COLUMN IF NOT EXISTS source_service VARCHAR(32) DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(128),
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pay_url TEXT;

-- Unique partial index on idempotency_key (only non-null values)
CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_idempotency
    ON billing.payment_orders(idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Index for stale order reconciliation (pending orders by expiry)
CREATE INDEX IF NOT EXISTS idx_orders_pending_expires
    ON billing.payment_orders(expires_at)
    WHERE status = 'pending';

-- Index for source service filtering
CREATE INDEX IF NOT EXISTS idx_orders_source
    ON billing.payment_orders(source_service);

-- 2. Pre-authorization holds for streaming API calls
CREATE TABLE IF NOT EXISTS billing.wallet_pre_authorizations (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL,
    wallet_id       BIGINT NOT NULL,
    amount          DECIMAL(14,4) NOT NULL,        -- frozen amount
    actual_amount   DECIMAL(14,4),                 -- settled amount (NULL until settled)
    status          VARCHAR(16) DEFAULT 'active',  -- active/settled/released/expired
    product_id      VARCHAR(32) NOT NULL,          -- requesting product (e.g. lurus-api)
    reference_id    VARCHAR(128),                  -- product-specific ref (e.g. request UUID)
    description     TEXT,
    expires_at      TIMESTAMPTZ NOT NULL,          -- auto-release deadline
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    settled_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_preauth_account
    ON billing.wallet_pre_authorizations(account_id);
CREATE INDEX IF NOT EXISTS idx_preauth_status
    ON billing.wallet_pre_authorizations(status);
CREATE INDEX IF NOT EXISTS idx_preauth_expires
    ON billing.wallet_pre_authorizations(expires_at)
    WHERE status = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS idx_preauth_reference
    ON billing.wallet_pre_authorizations(product_id, reference_id)
    WHERE reference_id IS NOT NULL;
