-- 009_outbox_events.sql
-- Transactional outbox table for reliable NATS event delivery.
-- Events are inserted in the same DB transaction as the business state change,
-- then a background relay publishes them to NATS and marks them as published.

CREATE TABLE IF NOT EXISTS identity.outbox_events (
    id            BIGSERIAL      PRIMARY KEY,
    event_id      VARCHAR(36)    NOT NULL,
    subject       VARCHAR(128)   NOT NULL,
    payload       JSONB          NOT NULL,
    created_at    TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    published_at  TIMESTAMPTZ,
    attempts      INT            NOT NULL DEFAULT 0,
    last_error    TEXT
);

-- Fast lookup for unpublished events (relay polling).
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished
    ON identity.outbox_events (id) WHERE published_at IS NULL;

-- Retention cleanup: find published events older than cutoff.
CREATE INDEX IF NOT EXISTS idx_outbox_published_at
    ON identity.outbox_events (published_at) WHERE published_at IS NOT NULL;
