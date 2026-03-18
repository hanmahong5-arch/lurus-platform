-- Migration 004: Seed retention-related notification templates
-- (checkin, milestones, referral, weekly digest)
-- Run: psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/004_retention_templates.sql

BEGIN;

INSERT INTO notification.templates (event_type, channel, title, body, priority) VALUES
    -- Daily check-in success
    ('identity.checkin.success', 'in_app',
     'Check-in Successful',
     'You checked in today! Current streak: {{streak}} days.',
     'normal'),

    -- Check-in milestone (7/30/100 days)
    ('identity.checkin.milestone', 'in_app',
     'Streak Milestone!',
     'Congratulations! You have checked in for {{milestone}} consecutive days!',
     'high'),

    -- Referral sign-up notification
    ('identity.referral.signup', 'in_app',
     'Referral Success',
     '{{referred_name}} has joined Lurus using your referral link!',
     'normal'),

    -- Weekly usage digest
    ('system.weekly_digest', 'email',
     'Your Lurus Weekly Summary',
     'Hi {{display_name}}, here is your weekly activity summary: {{api_calls}} API calls, {{tokens_used}} tokens used.',
     'low')
ON CONFLICT (event_type, channel) DO NOTHING;

COMMIT;
