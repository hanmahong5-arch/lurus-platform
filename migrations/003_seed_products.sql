-- Migration 003: seed initial products and plans
-- Seeds llm-api, quant-trading, webmail products with their free/paid tiers.

-- VIP level configuration
INSERT INTO identity.vip_level_configs (level, name, min_spend_cny, yearly_sub_min_plan, global_discount, perks_json)
VALUES
    (0, 'Standard',  0,       NULL,       1.000, '{}'),
    (1, 'Silver',    500,     'basic',    0.980, '{"priority_support": false}'),
    (2, 'Gold',      2000,    'pro',      0.950, '{"priority_support": true}'),
    (3, 'Platinum',  10000,   'pro',      0.920, '{"priority_support": true, "dedicated_manager": false}'),
    (4, 'Diamond',   50000,   'enterprise', 0.900, '{"priority_support": true, "dedicated_manager": true}')
ON CONFLICT (level) DO NOTHING;

-- ===================== llm-api =====================
INSERT INTO identity.products (id, name, description, category, billing_model, status, sort_order)
VALUES ('llm-api', 'LLM API', 'Unified LLM gateway with quota and channel routing', 'ai', 'hybrid', 1, 10)
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity.product_plans (product_id, code, name, billing_cycle, price_cny, price_usd, is_default, sort_order, status, features)
VALUES
    ('llm-api', 'free',       '免费版',   'forever',   0,    0,    true,  10, 1, '{"daily_quota":500000,"model_group":"free","topup_bonus_rate":0}'),
    ('llm-api', 'basic',      '基础版',   'monthly',   29,   4,    false, 20, 1, '{"daily_quota":2000000,"model_group":"basic","topup_bonus_rate":0.05}'),
    ('llm-api', 'pro',        '专业版',   'monthly',   99,   14,   false, 30, 1, '{"daily_quota":5000000,"model_group":"pro","topup_bonus_rate":0.10}'),
    ('llm-api', 'pro_yearly', '专业版年付','yearly',   999,  139,  false, 31, 1, '{"daily_quota":5000000,"model_group":"pro","topup_bonus_rate":0.15}'),
    ('llm-api', 'enterprise', '企业版',   'monthly',   399,  55,   false, 40, 1, '{"daily_quota":-1,"model_group":"enterprise","topup_bonus_rate":0.20}')
ON CONFLICT (product_id, code) DO NOTHING;

-- ===================== quant-trading =====================
INSERT INTO identity.products (id, name, description, category, billing_model, status, sort_order)
VALUES ('quant-trading', '量化交易', 'AI-driven quant strategy platform', 'trading', 'subscription', 1, 20)
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity.product_plans (product_id, code, name, billing_cycle, price_cny, price_usd, is_default, sort_order, status, features)
VALUES
    ('quant-trading', 'free',   '体验版', 'forever',  0,    0,    true,  10, 1, '{"max_live_strategies":1,"real_money":false,"backtest_years":1}'),
    ('quant-trading', 'basic',  '基础版', 'monthly',  99,   14,   false, 20, 1, '{"max_live_strategies":3,"real_money":true,"backtest_years":3}'),
    ('quant-trading', 'pro',    '专业版', 'monthly',  299,  42,   false, 30, 1, '{"max_live_strategies":5,"real_money":true,"backtest_years":5}'),
    ('quant-trading', 'enterprise', '企业版', 'monthly', 999, 139, false, 40, 1, '{"max_live_strategies":-1,"real_money":true,"backtest_years":10}')
ON CONFLICT (product_id, code) DO NOTHING;

-- ===================== webmail =====================
INSERT INTO identity.products (id, name, description, category, billing_model, status, sort_order)
VALUES ('webmail', '企业邮箱', 'Email, calendar, contacts and file storage', 'communication', 'subscription', 1, 30)
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity.product_plans (product_id, code, name, billing_cycle, price_cny, price_usd, is_default, sort_order, status, features)
VALUES
    ('webmail', 'free',   '免费版', 'forever',  0,   0,   true,  10, 1, '{"storage_gb":1,"max_aliases":2,"custom_domain":false}'),
    ('webmail', 'basic',  '基础版', 'monthly',  19,  3,   false, 20, 1, '{"storage_gb":10,"max_aliases":10,"custom_domain":false}'),
    ('webmail', 'pro',    '专业版', 'monthly',  49,  7,   false, 30, 1, '{"storage_gb":50,"max_aliases":20,"custom_domain":true}'),
    ('webmail', 'enterprise', '企业版', 'monthly', 199, 28, false, 40, 1, '{"storage_gb":500,"max_aliases":-1,"custom_domain":true}')
ON CONFLICT (product_id, code) DO NOTHING;
