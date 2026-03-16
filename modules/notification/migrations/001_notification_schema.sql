-- Migration 001: Notification service schema
-- Run: psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/001_notification_schema.sql

BEGIN;

CREATE SCHEMA IF NOT EXISTS notification;

-- Notification log: every notification sent across all channels
CREATE TABLE IF NOT EXISTS notification.notifications (
    id          BIGSERIAL PRIMARY KEY,
    account_id  BIGINT NOT NULL,
    channel     VARCHAR(20) NOT NULL,  -- in_app, email, fcm
    category    VARCHAR(50) NOT NULL,  -- account, subscription, strategy, risk, quota
    title       VARCHAR(200) NOT NULL,
    body        TEXT NOT NULL,
    priority    VARCHAR(20) NOT NULL DEFAULT 'normal',
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    event_type  VARCHAR(100),
    event_id    VARCHAR(50),
    metadata    JSONB DEFAULT '{}',
    read_at     TIMESTAMPTZ,
    sent_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_account_id ON notification.notifications (account_id);
CREATE INDEX IF NOT EXISTS idx_notifications_event_id ON notification.notifications (event_id);
CREATE INDEX IF NOT EXISTS idx_notifications_account_channel_unread
    ON notification.notifications (account_id, channel) WHERE read_at IS NULL;

-- Templates: define title/body for each event_type + channel combo
CREATE TABLE IF NOT EXISTS notification.templates (
    id          BIGSERIAL PRIMARY KEY,
    event_type  VARCHAR(100) NOT NULL,
    channel     VARCHAR(20) NOT NULL,
    title       VARCHAR(200) NOT NULL,
    body        TEXT NOT NULL,
    priority    VARCHAR(20) NOT NULL DEFAULT 'normal',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(event_type, channel)
);

-- User preferences: per-account channel toggle
CREATE TABLE IF NOT EXISTS notification.preferences (
    id          BIGSERIAL PRIMARY KEY,
    account_id  BIGINT NOT NULL,
    channel     VARCHAR(20) NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(account_id, channel)
);

-- Device tokens for FCM/APNs push (Sprint D)
CREATE TABLE IF NOT EXISTS notification.device_tokens (
    id          BIGSERIAL PRIMARY KEY,
    account_id  BIGINT NOT NULL,
    platform    VARCHAR(20) NOT NULL,  -- ios, android
    token       VARCHAR(500) NOT NULL UNIQUE,
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_device_tokens_account ON notification.device_tokens (account_id);

COMMIT;
