-- =====================================================================
-- Migration 005: Invoices and Refunds
-- =====================================================================

CREATE TABLE IF NOT EXISTS billing.invoices (
    id           BIGSERIAL      PRIMARY KEY,
    invoice_no   VARCHAR(32)    UNIQUE NOT NULL,
    account_id   BIGINT         NOT NULL,
    order_no     VARCHAR(64)    UNIQUE NOT NULL,
    issue_date   TIMESTAMPTZ    NOT NULL,
    line_items   JSONB          NOT NULL DEFAULT '[]',
    subtotal_cny NUMERIC(12,2)  NOT NULL,
    total_cny    NUMERIC(12,2)  NOT NULL,
    currency     VARCHAR(3)     NOT NULL DEFAULT 'CNY',
    status       VARCHAR(16)    NOT NULL DEFAULT 'issued',
    created_at   TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_invoices_account ON billing.invoices(account_id);

CREATE TABLE IF NOT EXISTS billing.refunds (
    id           BIGSERIAL      PRIMARY KEY,
    refund_no    VARCHAR(32)    UNIQUE NOT NULL,
    account_id   BIGINT         NOT NULL,
    order_no     VARCHAR(64)    NOT NULL,
    amount_cny   NUMERIC(12,2)  NOT NULL,
    reason       TEXT           NOT NULL,
    status       VARCHAR(16)    NOT NULL DEFAULT 'pending',
    review_note  TEXT,
    reviewed_by  VARCHAR(64),
    reviewed_at  TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_refunds_account ON billing.refunds(account_id);
CREATE INDEX IF NOT EXISTS idx_refunds_order   ON billing.refunds(order_no);
