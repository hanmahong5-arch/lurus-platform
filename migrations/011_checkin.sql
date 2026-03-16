-- 011_checkin.sql
-- Daily check-in tracking table for wallet reward system.

CREATE TABLE IF NOT EXISTS identity.checkins (
    id           BIGSERIAL PRIMARY KEY,
    account_id   BIGINT NOT NULL,
    checkin_date VARCHAR(10) NOT NULL,
    reward_type  VARCHAR(32) NOT NULL DEFAULT 'credits',
    reward_value DECIMAL(14,4) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_checkin_daily UNIQUE(account_id, checkin_date)
);

CREATE INDEX IF NOT EXISTS idx_checkin_account
    ON identity.checkins(account_id);
