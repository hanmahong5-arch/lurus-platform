CREATE USER rls_client
WITH LOGIN PASSWORD 'vZEkBcx4jEJ4J6I' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;

GRANT anon TO rls_client;

GRANT authenticated TO rls_client;

-- REVOKE ALL ON SCHEMA public FROM PUBLIC;
-- GRANT USAGE ON SCHEMA public TO rls_client;


-- Use Postgres to create a bucket.

insert into storage.buckets (id, name, public)
values ('attachments', 'attachments', false)
    on conflict (id) do nothing;


-- Private: owner-only CRUD
create policy "private-own-crud"
on storage.objects
for all to authenticated
using (
  bucket_id = 'attachments'
  and name like 'private/' || auth.uid()::text || '/%'
)
with check (
  bucket_id = 'attachments'
  and name like 'private/' || auth.uid()::text || '/%'
);

create policy "eml-own-crud"
on storage.objects
for all to authenticated
using (
  bucket_id = 'attachments'
  and name like 'eml/' || auth.uid()::text || '/%'
)
with check (
  bucket_id = 'attachments'
  and name like 'eml/' || auth.uid()::text || '/%'
);

-- -- Public: anyone can GET, only service_role (or your server) can write
-- create policy "public-read"
-- on storage.objects
-- for select to public
--                       using (bucket_id = 'attachments' and name like 'public/%');
--
-- -- If you want client-side uploads to public by logged-in users, scope it:
-- -- (else, omit and do all public writes with service_role)
-- create policy "public-insert-by-server-only"
-- on storage.objects
-- for insert to service_role
-- with check (bucket_id = 'attachments' and name like 'public/%');
--
--
-- create policy "public-own-insert"
-- on storage.objects
-- for insert to authenticated
-- with check (
--   bucket_id = 'attachments'
--   and name like 'public/' || auth.uid()::text || '/%'
-- );
