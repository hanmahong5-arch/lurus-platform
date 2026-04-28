-- 026_api_keys.sql
-- Lurus 应用密钥（API key）管理表 — 抽象掉 Zitadel "Service User + PAT" 的概念。
--
-- 设计目标：
--   - 中文运维者只需关心"应用密钥"概念：name + purpose + token
--   - 不暴露 Zitadel 的 Service User / Personal Access Token / IAM_OWNER 模型
--   - 幂等创建：相同 name 二次创建返回现有 row（不重新生成 token）
--   - rotate / revoke 提供原子换 token 的操作
--   - 失败回滚：Zitadel 端创建失败时标记 status='failed'，下次重试可清理重建
--
-- name 是幂等键。purpose 是分类标签（login_ui / mcp / external / admin）。

CREATE TABLE IF NOT EXISTS identity.api_keys (
    id                BIGSERIAL    PRIMARY KEY,

    -- 业务唯一标识。name 是稳定的，比如 "login-ui" / "platform-mcp" /
    -- "external-monitoring"；UI 用 display_name 显示，name 用作幂等键。
    name              VARCHAR(64)  NOT NULL UNIQUE,
    display_name      VARCHAR(128) NOT NULL,

    -- 用途分类。约束在应用层校验（避免 enum 升级时改 DDL）。
    --   login_ui  — Lurus Login UI 服务账号
    --   mcp       — MCP server (zitadel-mcp / k8s-mcp / platform-mcp)
    --   external  — 外部集成（监控、第三方）
    --   admin     — 内部管理工具
    purpose           VARCHAR(32)  NOT NULL,

    -- Zitadel 侧的 ID。未创建成功前为空（status='creating' 阶段）。
    zitadel_user_id   VARCHAR(64),
    -- Zitadel 端 PAT 的 ID，用于 rotate / revoke 时定位。
    zitadel_token_id  VARCHAR(64),

    -- 状态机：
    --   creating — DB row 已建，Zitadel 调用 in-flight
    --   active   — Zitadel User + PAT 已建好（zitadel_user_id 必非空）
    --   failed   — Zitadel 调用失败；下次创建同 name 时允许清理重试
    --   revoked  — 用户主动删除（保留行作审计）
    status            VARCHAR(16)  NOT NULL DEFAULT 'creating',

    -- 失败原因（status='failed' 时填）。截断到 1 KB 防 stack-trace 灌爆。
    error             TEXT,

    -- token hash — 验证调用方知道 token 而不存储明文。
    -- 实际 token 不存（只创建时返回一次给 API caller）。
    token_hash        VARCHAR(64),

    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    revoked_at        TIMESTAMPTZ,

    -- created_by 是调用 /admin/v1/api-keys 的管理员账号 id（可空：迁移过来的旧记录）
    created_by        BIGINT,

    CONSTRAINT fk_api_keys_creator
        FOREIGN KEY (created_by) REFERENCES identity.accounts(id) ON DELETE SET NULL
);

-- List 查询主索引。
CREATE INDEX IF NOT EXISTS idx_api_keys_status_created
    ON identity.api_keys (status, created_at DESC);

-- purpose 过滤（"列出所有 mcp 类密钥"）。
CREATE INDEX IF NOT EXISTS idx_api_keys_purpose
    ON identity.api_keys (purpose);

-- updated_at 自动更新触发器（GORM/手写 SQL 都覆盖）。
CREATE OR REPLACE FUNCTION identity.api_keys_touch_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS api_keys_touch_updated_at ON identity.api_keys;
CREATE TRIGGER api_keys_touch_updated_at
    BEFORE UPDATE ON identity.api_keys
    FOR EACH ROW
    EXECUTE FUNCTION identity.api_keys_touch_updated_at();
