-- Migration 005: add source + payload columns to notifications
-- Run: psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/005_source_and_payload.sql
--
-- Idempotent: every CREATE/ALTER/UPDATE uses IF NOT EXISTS or guards so a
-- second run on the same database is a no-op.

BEGIN;

-- New columns on the existing notifications table.
--   source  = first dot-segment of event_type (identity|lucrum|llm|psi)
--   payload = client-facing event data, distinct from internal `metadata`
--             which keeps tracking failure reasons.
ALTER TABLE notification.notifications
    ADD COLUMN IF NOT EXISTS source VARCHAR(20) NOT NULL DEFAULT 'identity',
    ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}';

-- Backfill source from event_type prefix for rows still on the default.
-- Safe to re-run: only touches rows that haven't been backfilled.
UPDATE notification.notifications
   SET source = split_part(event_type, '.', 1)
 WHERE source = 'identity'
   AND event_type LIKE '%.%.%'
   AND split_part(event_type, '.', 1) <> 'identity';

-- Per-source unread aggregation index (replaces the by-type filter pattern
-- the client used to do in memory).
CREATE INDEX IF NOT EXISTS idx_notifications_account_source_unread
    ON notification.notifications (account_id, source) WHERE read_at IS NULL;

COMMIT;
