-- Migration 024: seed Lurus Tally product + plans
-- Adds the lurus-tally entry so that POST /internal/v1/subscriptions/checkout
-- can resolve plan_code+billing_cycle when called from 2b-svc-psi (Tally).
--
-- Plans align with Tally enterprise positioning (free tier for trial, paid tiers
-- gated by feature_set). Prices are introductory; admin can adjust via /admin/v1/plans.

INSERT INTO identity.products (id, name, description, category, billing_model, status, sort_order)
VALUES ('lurus-tally', 'Lurus Tally', 'AI-native 进销存 SaaS — 商品/库存/采购/销售/财务一体化', 'saas', 'subscription', 1, 25)
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity.product_plans (product_id, code, name, billing_cycle, price_cny, price_usd, is_default, sort_order, status, features)
VALUES
    ('lurus-tally', 'free',           '免费版',     'forever',  0,    0,    true,  10, 1, '{"max_skus":50,"max_users":1,"warehouses":1,"ai_assistant":false,"reports":"basic"}'),
    ('lurus-tally', 'pro',            '专业版',     'monthly',  199,  28,   false, 20, 1, '{"max_skus":2000,"max_users":5,"warehouses":3,"ai_assistant":true,"reports":"advanced"}'),
    ('lurus-tally', 'pro_yearly',     '专业版年付', 'yearly',   1990, 280,  false, 21, 1, '{"max_skus":2000,"max_users":5,"warehouses":3,"ai_assistant":true,"reports":"advanced"}'),
    ('lurus-tally', 'enterprise',     '企业版',     'monthly',  599,  84,   false, 30, 1, '{"max_skus":-1,"max_users":-1,"warehouses":-1,"ai_assistant":true,"reports":"premium","priority_support":true}'),
    ('lurus-tally', 'enterprise_yearly','企业版年付','yearly',  5990, 840,  false, 31, 1, '{"max_skus":-1,"max_users":-1,"warehouses":-1,"ai_assistant":true,"reports":"premium","priority_support":true}')
ON CONFLICT (product_id, code) DO NOTHING;
