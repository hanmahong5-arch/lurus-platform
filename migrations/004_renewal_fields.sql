-- Idempotent migration: add renewal tracking columns to billing.subscriptions
ALTER TABLE identity.subscriptions
  ADD COLUMN IF NOT EXISTS renewal_attempts    INT         NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS next_renewal_at     TIMESTAMPTZ;
