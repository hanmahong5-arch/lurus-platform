-- 023_zitadel_sub_unique_partial.sql
-- Fix a latent data bug: identity.accounts.zitadel_sub has a plain UNIQUE
-- constraint, meaning empty string '' can only exist on one row. The first
-- admin/bootstrap account created before Zitadel is configured (or with
-- WeChat login which doesn't populate zitadel_sub) sets zitadel_sub = '',
-- and any subsequent such registration fails with "duplicate key".
--
-- Fix: match the pattern already used for email / phone / username —
-- partial UNIQUE index excluding NULL and empty string. Also backfill
-- existing empty-strings to NULL for consistency.
--
-- Pre-check (confirmed on prod 2026-04-23): 1 row with zitadel_sub = ''.
-- Safe to convert to NULL — no code reads empty zitadel_sub for identity.
--
-- Idempotent: each step can run multiple times.

-- 1. Normalize existing empty strings to NULL.
UPDATE identity.accounts
    SET zitadel_sub = NULL
    WHERE zitadel_sub = '';

-- 2. Drop the plain UNIQUE constraint (it lives as a constraint + backing
--    unique index — drop via ALTER TABLE so both go together).
ALTER TABLE identity.accounts
    DROP CONSTRAINT IF EXISTS accounts_zitadel_sub_key;

-- 3. Add a partial UNIQUE index — NULL and '' are allowed to repeat.
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_zitadel_sub_unique
    ON identity.accounts (zitadel_sub)
    WHERE zitadel_sub IS NOT NULL
      AND zitadel_sub <> '';
