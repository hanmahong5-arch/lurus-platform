-- Migration 003: Seed multi-threshold quota alert notification templates
-- Run: psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/003_quota_alert_templates.sql

BEGIN;

INSERT INTO notification.templates (event_type, channel, title, body, priority) VALUES
    -- Quota 50% threshold
    ('llm.quota.50', 'in_app',
     'Usage at 50%',
     'Your API quota has reached {{percent}}%. You have {{remaining}} tokens remaining this month.',
     'normal'),

    -- Quota 80% threshold
    ('llm.quota.80', 'in_app',
     'Usage at 80%',
     'Your API quota has reached {{percent}}%. Only {{remaining}} tokens remaining. Consider upgrading your plan.',
     'high'),
    ('llm.quota.80', 'email',
     'API Quota Warning: 80% Used',
     'Your Lurus API quota has reached {{percent}}%. You have {{remaining}} tokens remaining this month. Upgrade at https://identity.lurus.cn/pricing to avoid service interruption.',
     'high'),

    -- Quota 95% threshold
    ('llm.quota.95', 'in_app',
     'Usage at 95% — Almost Exhausted',
     'Your API quota has reached {{percent}}%. Only {{remaining}} tokens remaining. Upgrade now to avoid interruption.',
     'urgent'),
    ('llm.quota.95', 'email',
     'Urgent: API Quota Almost Exhausted (95%)',
     'Your Lurus API quota is at {{percent}}% with only {{remaining}} tokens remaining. Your API access will be suspended when the quota is exhausted. Please upgrade at https://identity.lurus.cn/pricing.',
     'urgent'),

    -- Quota 100% threshold (exhausted)
    ('llm.quota.100', 'in_app',
     'Quota Exhausted',
     'Your API quota has been fully consumed. Please upgrade your plan or top up credits to resume service.',
     'urgent'),
    ('llm.quota.100', 'email',
     'API Quota Exhausted — Service Suspended',
     'Your Lurus API quota has been fully consumed. Your API access is now suspended. Please upgrade your plan or top up credits at https://identity.lurus.cn/pricing to resume service.',
     'urgent')
ON CONFLICT (event_type, channel) DO NOTHING;

COMMIT;
