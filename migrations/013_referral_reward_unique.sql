-- 013_referral_reward_unique.sql
-- Prevent duplicate referral rewards (same referrer+referee+event_type).
-- Idempotent: IF NOT EXISTS.

ALTER TABLE billing.referral_reward_events
    ADD CONSTRAINT uq_referral_reward_event
    UNIQUE (referrer_id, referee_id, event_type);
