-- 027_account_newapi_user_id.sql
-- 为 identity.accounts 加 newapi_user_id 列 — 支撑 C.2 NewAPI 计费同步集成
-- （见 docs/ADR-newapi-billing-sync.md）。
--
-- 心智模型：每个 platform account 对应至多一个 NewAPI user。NULL = 尚未同步
-- （比如老账户、或同步任务还没跑、或同步失败待重试）。
--
-- 1:1 映射：UNIQUE 约束防止两个 platform account 误绑同一个 NewAPI user。

ALTER TABLE identity.accounts
    ADD COLUMN IF NOT EXISTS newapi_user_id INT;

-- 1:1 反查索引。允许 NULL 多行（部分唯一约束 — 仅对非 NULL 值唯一）。
-- WHERE 子句让索引只对真正同步过的行生效，未同步的 NULL 行不互相冲突。
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_newapi_user_id_unique
    ON identity.accounts(newapi_user_id)
    WHERE newapi_user_id IS NOT NULL;
