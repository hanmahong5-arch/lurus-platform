-- User preferences for cross-device sync (e.g. Creator model usage stats).
-- Each account can have multiple preference namespaces (e.g. "creator", "lucrum").
CREATE TABLE IF NOT EXISTS identity.user_preferences (
  id              BIGSERIAL PRIMARY KEY,
  account_id      BIGINT       NOT NULL REFERENCES identity.accounts(id) ON DELETE CASCADE,
  namespace       VARCHAR(64)  NOT NULL DEFAULT 'default',  -- e.g. "creator", "lucrum"
  data            JSONB        NOT NULL DEFAULT '{}',
  updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  UNIQUE(account_id, namespace)
);

CREATE INDEX IF NOT EXISTS idx_user_preferences_account ON identity.user_preferences(account_id);
