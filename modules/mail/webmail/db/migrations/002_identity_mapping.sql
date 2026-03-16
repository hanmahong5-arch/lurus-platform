-- Identity Mapping Table for Unified User Management
-- Links Zitadel users to Webmail (Supabase) users for SSO

-- Identity provider type
CREATE TYPE "public"."identity_provider" AS ENUM('zitadel', 'supabase', 'lurus_api');

-- Main identity mapping table
CREATE TABLE "public"."identity_mapping" (
    "id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
    "zitadel_user_id" text UNIQUE,                      -- Zitadel user ID
    "supabase_user_id" uuid UNIQUE REFERENCES auth.users(id) ON DELETE CASCADE,
    "lurus_api_user_id" integer,                        -- Lurus-API user ID (NEW-API)
    "email" text NOT NULL,
    "display_name" text,
    "avatar_url" text,
    "primary_provider" "identity_provider" DEFAULT 'supabase' NOT NULL,
    "linked_at" timestamptz DEFAULT now() NOT NULL,
    "last_sync_at" timestamptz DEFAULT now(),
    "metadata" jsonb DEFAULT '{}'::jsonb,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

-- Enable RLS
ALTER TABLE "public"."identity_mapping" ENABLE ROW LEVEL SECURITY;

-- Index for fast lookups
CREATE INDEX "idx_identity_mapping_email" ON "public"."identity_mapping" ("email");
CREATE INDEX "idx_identity_mapping_zitadel_user_id" ON "public"."identity_mapping" ("zitadel_user_id");
CREATE INDEX "idx_identity_mapping_supabase_user_id" ON "public"."identity_mapping" ("supabase_user_id");

-- RLS Policies
CREATE POLICY "Users can view their own identity mapping"
    ON "public"."identity_mapping"
    FOR SELECT
    USING (supabase_user_id = auth.uid());

CREATE POLICY "Service role can manage all identity mappings"
    ON "public"."identity_mapping"
    FOR ALL
    USING (auth.jwt() ->> 'role' = 'service_role');

-- Trigger to update updated_at on change
CREATE OR REPLACE FUNCTION update_identity_mapping_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER identity_mapping_updated_at
    BEFORE UPDATE ON "public"."identity_mapping"
    FOR EACH ROW
    EXECUTE FUNCTION update_identity_mapping_timestamp();

-- SSO Session tracking table
CREATE TABLE "public"."sso_sessions" (
    "id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
    "identity_mapping_id" uuid NOT NULL REFERENCES "public"."identity_mapping"(id) ON DELETE CASCADE,
    "provider" "identity_provider" NOT NULL,
    "provider_session_id" text,
    "access_token_hash" text,                          -- SHA-256 hash, not raw token
    "refresh_token_hash" text,
    "expires_at" timestamptz NOT NULL,
    "ip_address" inet,
    "user_agent" text,
    "created_at" timestamptz DEFAULT now() NOT NULL
);

-- Enable RLS
ALTER TABLE "public"."sso_sessions" ENABLE ROW LEVEL SECURITY;

-- Index for session lookups
CREATE INDEX "idx_sso_sessions_identity_mapping_id" ON "public"."sso_sessions" ("identity_mapping_id");
CREATE INDEX "idx_sso_sessions_expires_at" ON "public"."sso_sessions" ("expires_at");

-- RLS Policies for sso_sessions
CREATE POLICY "Users can view their own SSO sessions"
    ON "public"."sso_sessions"
    FOR SELECT
    USING (identity_mapping_id IN (
        SELECT id FROM "public"."identity_mapping" WHERE supabase_user_id = auth.uid()
    ));

CREATE POLICY "Service role can manage all SSO sessions"
    ON "public"."sso_sessions"
    FOR ALL
    USING (auth.jwt() ->> 'role' = 'service_role');

-- Function to auto-create identity mapping on new user signup
CREATE OR REPLACE FUNCTION handle_new_user_identity_mapping()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO "public"."identity_mapping" (
        supabase_user_id,
        email,
        display_name,
        primary_provider
    ) VALUES (
        NEW.id,
        NEW.email,
        COALESCE(NEW.raw_user_meta_data->>'display_name', NEW.raw_user_meta_data->>'name', split_part(NEW.email, '@', 1)),
        'supabase'
    )
    ON CONFLICT (supabase_user_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Trigger to auto-create identity mapping on new user
CREATE TRIGGER on_auth_user_created_identity_mapping
    AFTER INSERT ON auth.users
    FOR EACH ROW
    EXECUTE FUNCTION handle_new_user_identity_mapping();

-- Function to link Zitadel user after SSO login
CREATE OR REPLACE FUNCTION link_zitadel_user(
    p_supabase_user_id uuid,
    p_zitadel_user_id text,
    p_email text,
    p_display_name text DEFAULT NULL
)
RETURNS uuid AS $$
DECLARE
    v_mapping_id uuid;
BEGIN
    -- Try to update existing mapping
    UPDATE "public"."identity_mapping"
    SET
        zitadel_user_id = p_zitadel_user_id,
        display_name = COALESCE(p_display_name, display_name),
        last_sync_at = now(),
        updated_at = now()
    WHERE supabase_user_id = p_supabase_user_id
    RETURNING id INTO v_mapping_id;

    -- If no mapping exists, create one
    IF v_mapping_id IS NULL THEN
        INSERT INTO "public"."identity_mapping" (
            supabase_user_id,
            zitadel_user_id,
            email,
            display_name,
            primary_provider
        ) VALUES (
            p_supabase_user_id,
            p_zitadel_user_id,
            p_email,
            COALESCE(p_display_name, split_part(p_email, '@', 1)),
            'zitadel'
        )
        RETURNING id INTO v_mapping_id;
    END IF;

    RETURN v_mapping_id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Grant permissions
GRANT USAGE ON SCHEMA public TO authenticated;
GRANT SELECT ON "public"."identity_mapping" TO authenticated;
GRANT SELECT ON "public"."sso_sessions" TO authenticated;
GRANT ALL ON "public"."identity_mapping" TO service_role;
GRANT ALL ON "public"."sso_sessions" TO service_role;
GRANT EXECUTE ON FUNCTION link_zitadel_user TO service_role;
