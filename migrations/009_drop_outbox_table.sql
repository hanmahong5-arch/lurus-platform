-- Migration 009: Drop outbox_events table.
-- The transactional outbox pattern has been replaced by Temporal workflow activities
-- which guarantee event delivery via Temporal's persistence layer.
--
-- Run AFTER confirming Temporal workflows are stable in production.
-- This migration is irreversible — ensure no code references identity.outbox_events.

BEGIN;

DROP TABLE IF EXISTS identity.outbox_events;

COMMIT;
