-- Migration 006: Seed templates for the 8 new unified-notification event types
-- Run: psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/006_seed_templates_v2.sql
--
-- Idempotent via ON CONFLICT (event_type, channel) DO NOTHING.

BEGIN;

INSERT INTO notification.templates (event_type, channel, title, body, priority) VALUES
    -- VIP level changed (identity)
    ('identity.vip.level_changed', 'in_app',
     'VIP Level Updated',
     'Your VIP level is now {{level}}. Enjoy your new perks!',
     'normal'),

    -- Lucrum advisor output
    ('lucrum.advisor.output', 'in_app',
     'Advisor Insight',
     '{{advisor_name}} on {{symbol}}: {{summary}}',
     'normal'),

    -- Lucrum market event
    ('lucrum.market.event', 'in_app',
     'Market Event',
     '{{symbol}}: {{headline}}',
     'high'),

    -- LLM image generated
    ('llm.image.generated', 'in_app',
     'Image Ready',
     'Your image generation finished. Tap to view.',
     'normal'),
    ('llm.image.generated', 'fcm',
     'Image Ready',
     'Your generated image is ready.',
     'normal'),

    -- LLM usage milestone (per-message digest candidate; for now real-time)
    ('llm.usage.milestone', 'in_app',
     'Usage Milestone',
     'You have used {{tokens_used}} tokens this {{period}}.',
     'low'),

    -- PSI: order needs approval
    ('psi.order.approval_needed', 'in_app',
     'Order Needs Approval',
     'Order {{order_no}} (CNY {{amount_cny}}) submitted by {{submitted_by}} is waiting for your approval.',
     'high'),
    ('psi.order.approval_needed', 'fcm',
     'Order Needs Approval',
     'Order {{order_no}} ({{amount_cny}} CNY) needs your approval.',
     'high'),

    -- PSI: inventory hit redline
    ('psi.inventory.redline', 'in_app',
     'Low Stock Alert',
     '{{sku_name}} ({{sku}}) on hand: {{on_hand}}, below threshold {{threshold}}.',
     'high'),

    -- PSI: payment received
    ('psi.payment.received', 'in_app',
     'Payment Received',
     'Received CNY {{amount_cny}} from {{payer_name}}.',
     'normal')
ON CONFLICT (event_type, channel) DO NOTHING;

COMMIT;
