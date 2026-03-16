-- Create roles if not exist
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'anon') THEN
    CREATE ROLE anon NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'service_role') THEN
    CREATE ROLE service_role NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authenticator') THEN
    CREATE ROLE authenticator LOGIN PASSWORD 'LurusOps2026';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_auth_admin') THEN
    CREATE ROLE supabase_auth_admin LOGIN PASSWORD 'LurusOps2026';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_storage_admin') THEN
    CREATE ROLE supabase_storage_admin LOGIN PASSWORD 'LurusOps2026';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'rls_client') THEN
    CREATE ROLE rls_client LOGIN PASSWORD 'LurusRls2026';
  END IF;
END
$$;

-- Grant permissions
GRANT anon, authenticated, service_role TO authenticator;
GRANT ALL ON DATABASE webmail TO supabase_auth_admin;
GRANT ALL ON DATABASE webmail TO supabase_storage_admin;
GRANT ALL ON DATABASE webmail TO rls_client;

-- Create schemas
CREATE SCHEMA IF NOT EXISTS auth;
CREATE SCHEMA IF NOT EXISTS storage;
CREATE SCHEMA IF NOT EXISTS graphql_public;

-- Grant schema permissions
GRANT USAGE ON SCHEMA public TO anon, authenticated, service_role;
GRANT USAGE ON SCHEMA auth TO supabase_auth_admin;
GRANT USAGE ON SCHEMA storage TO supabase_storage_admin;
GRANT ALL ON SCHEMA auth TO supabase_auth_admin;
GRANT ALL ON SCHEMA storage TO supabase_storage_admin;
