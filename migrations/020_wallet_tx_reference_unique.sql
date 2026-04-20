-- 020_wallet_tx_reference_unique.sql
-- Enforce idempotency at the DB layer for wallet transactions keyed by an
-- external reference (payment_order, subscription, redemption_code, etc).
--
-- Rationale: App-layer idempotency relies on conditional UPDATE in
-- MarkPaymentOrderPaid (didTransition gate). This protects the happy path,
-- but a direct Topup/Credit call or a future refactor bypass could still
-- double-credit. A UNIQUE index on (type, reference_type, reference_id)
-- makes duplicate insertion impossible at the database level.
--
-- Scope: partial index — only rows where both reference_type and reference_id
-- are non-empty. NULL/empty references (manual admin adjustments, bonus
-- grants with no external ref) stay unconstrained by design.
--
-- Pre-check (run before applying):
--   SELECT type, reference_type, reference_id, COUNT(*)
--   FROM billing.wallet_transactions
--   WHERE reference_id IS NOT NULL AND reference_type IS NOT NULL
--   GROUP BY type, reference_type, reference_id HAVING COUNT(*) > 1;
-- → Must return 0 rows, else fix data before running this migration.
--
-- Idempotent: IF NOT EXISTS.

CREATE UNIQUE INDEX IF NOT EXISTS idx_wtx_reference_unique
    ON billing.wallet_transactions (type, reference_type, reference_id)
    WHERE reference_id IS NOT NULL
      AND reference_type IS NOT NULL
      AND reference_id <> ''
      AND reference_type <> '';
