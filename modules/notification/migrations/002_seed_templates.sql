-- Migration 002: Seed default notification templates
-- Run: psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/002_seed_templates.sql

BEGIN;

INSERT INTO notification.templates (event_type, channel, title, body, priority) VALUES
    -- Account created
    ('identity.account.created', 'in_app', 'Welcome to Lurus', 'Your account has been created successfully. Welcome aboard!', 'normal'),
    ('identity.account.created', 'email', 'Welcome to Lurus', 'Hello! Your Lurus account is ready. Start exploring our platform at https://www.lurus.cn', 'normal'),

    -- Subscription activated
    ('identity.subscription.activated', 'in_app', 'Subscription Activated', 'Your {{plan_code}} plan is now active until {{expires_at}}.', 'normal'),
    ('identity.subscription.activated', 'email', 'Subscription Confirmed', 'Your {{plan_code}} subscription has been activated. It will remain active until {{expires_at}}.', 'normal'),

    -- Subscription expired
    ('identity.subscription.expired', 'in_app', 'Subscription Expired', 'Your subscription has expired. Renew to continue accessing premium features.', 'high'),
    ('identity.subscription.expired', 'email', 'Subscription Expired', 'Your Lurus subscription has expired. Please renew at https://identity.lurus.cn to restore access.', 'high'),

    -- Topup completed
    ('identity.topup.completed', 'in_app', 'Top-up Successful', '{{credits_added}} credits have been added to your wallet.', 'normal'),

    -- Strategy triggered
    ('lucrum.strategy.triggered', 'in_app', 'Strategy Signal', '{{strategy_name}}: {{signal}} signal on {{symbol}}', 'high'),
    ('lucrum.strategy.triggered', 'fcm', 'Strategy Signal', '{{strategy_name}}: {{signal}} on {{symbol}}', 'high'),

    -- Risk alert
    ('lucrum.risk.alert', 'in_app', 'Risk Alert', '{{alert_type}} alert on {{symbol}}: {{message}}', 'urgent'),
    ('lucrum.risk.alert', 'email', 'Urgent: Risk Alert', 'Risk alert on {{symbol}} ({{alert_type}}): {{message}}. Please check your positions.', 'urgent'),
    ('lucrum.risk.alert', 'fcm', 'Risk Alert', '{{symbol}}: {{message}}', 'urgent'),

    -- Quota threshold
    ('llm.quota.threshold', 'in_app', 'Usage Warning', 'Your API usage has reached {{usage_percent}} of your monthly quota.', 'high')
ON CONFLICT (event_type, channel) DO NOTHING;

COMMIT;
