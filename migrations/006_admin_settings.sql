-- Migration 006: admin settings table for runtime configuration
-- Allows administrators to update payment credentials without redeploying.

CREATE SCHEMA IF NOT EXISTS admin;

CREATE TABLE IF NOT EXISTS admin.settings (
    key         VARCHAR(128) PRIMARY KEY,
    value       TEXT         NOT NULL DEFAULT '',
    is_secret   BOOLEAN      NOT NULL DEFAULT false, -- masked as "••••••••" in API responses
    updated_by  VARCHAR(128) NOT NULL DEFAULT 'system',
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Pre-insert all configurable keys (empty values — admin fills them in via UI).
INSERT INTO admin.settings(key, is_secret) VALUES
  ('epay_partner_id',       false),
  ('epay_key',              true),
  ('epay_gateway_url',      false),
  ('epay_notify_url',       false),
  ('stripe_secret_key',     true),
  ('stripe_webhook_secret', true),
  ('creem_api_key',         true),
  ('creem_webhook_secret',  true),
  ('qr_static_alipay',      false),  -- base64 image data for Alipay static QR
  ('qr_static_wechat',      false),  -- base64 image data for WeChat static QR
  ('qr_channel_promo',      false)   -- base64 image data for channel promo QR
ON CONFLICT DO NOTHING;
