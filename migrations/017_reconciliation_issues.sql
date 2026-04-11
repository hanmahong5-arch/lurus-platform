-- Reconciliation issues: tracks discrepancies between payment orders and wallet credits.
-- Detected by ReconciliationWorker on each tick; resolved manually via admin.

CREATE TABLE IF NOT EXISTS billing.reconciliation_issues (
    id              BIGSERIAL PRIMARY KEY,
    issue_type      VARCHAR(32)    NOT NULL,              -- missing_credit, orphan_payment, amount_mismatch
    severity        VARCHAR(16)    NOT NULL DEFAULT 'warning', -- critical, warning, info
    order_no        VARCHAR(64),
    account_id      BIGINT,
    provider        VARCHAR(32),
    expected_amount DECIMAL(10,2),
    actual_amount   DECIMAL(10,2),
    description     TEXT           NOT NULL,
    status          VARCHAR(16)    NOT NULL DEFAULT 'open', -- open, resolved, ignored
    resolution      TEXT,
    detected_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recon_issues_status ON billing.reconciliation_issues(status);
CREATE INDEX IF NOT EXISTS idx_recon_issues_order_no ON billing.reconciliation_issues(order_no);
CREATE INDEX IF NOT EXISTS idx_recon_issues_detected_at ON billing.reconciliation_issues(detected_at);
