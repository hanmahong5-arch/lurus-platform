-- Migration 008: referral aff_code index + referral reward schema updates
-- Note: aff_code and referrer_id columns already exist in migration 001.
-- This migration ensures the named index exists and is idempotent.

-- Ensure a named index for direct aff_code lookups (referral link resolution).
CREATE INDEX IF NOT EXISTS idx_accounts_aff_code ON identity.accounts(aff_code);

-- Ensure the referral_reward_events table has a status index for quick lookups.
CREATE INDEX IF NOT EXISTS idx_referral_status ON billing.referral_reward_events(status)
    WHERE status IS NOT NULL;
