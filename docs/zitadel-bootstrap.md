# Zitadel Machine-Key Bootstrap

> **Goal**: wire a Zitadel service-account machine key into the platform so
> that `zitadel-admin.sh` (and anything else that cares) can drive the
> Management API via JWT-bearer-grant — **zero PATs stored, zero clicks per
> future op**.

## One-time console steps (2 minutes)

Open **https://auth.lurus.cn** and sign in:

- **Username**: `zitadel-admin@zitadel.auth.lurus.cn`
- **Password**: value of `zitadel-secrets.admin-password` in the `lurus-platform`
  namespace (currently `Lurus@ops` — rotate after bootstrap).

Then:

1. **Organization menu → Service Users → New**
   - Username: `platform-admin-sa`
   - First/Last/Display name: anything (e.g. `Platform` / `Admin SA` / `platform-admin`)
   - Access token type: **Bearer**

2. **Authorizations tab** (on the new user) **→ New**
   - Project: `ZITADEL` (default system project)
   - Role: **`IAM_OWNER`** (full admin) — alternatively `IAM_USER_MANAGER` + `IAM_PROJECT_OWNER` for least privilege

3. **Keys tab → New**
   - Type: **`JSON`** *(NOT "PEM (public only)" — we need the private key)*
   - Expiration: a year or whatever policy dictates
   - **Download the JSON** — this is the ONLY moment it's visible

That JSON looks like:
```json
{
  "type": "serviceaccount",
  "keyId": "30...",
  "key": "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----\n",
  "userId": "30..."
}
```

## One-shot install

```bash
cd 2l-svc-platform
./scripts/bootstrap-zitadel.sh /path/to/downloaded-sa.json
```

That script:
1. Validates the JSON has a private key.
2. Uploads it to the `zitadel-machinekey` k8s secret (overwrites the stale
   pubkey-only copy).
3. Calls `zitadel-admin.sh save-pat-to-secret <userId> ZITADEL_SERVICE_ACCOUNT_PAT`,
   which mints a fresh PAT via JWT-bearer-grant and patches the
   `platform-core-secrets` k8s secret.
4. `kubectl rollout restart deployment platform-core`.
5. Verifies `POST /api/v1/auth/login` now returns JSON (not HTML).

## Post-bootstrap admin ops (zero-click)

Everything is API-driven through `scripts/zitadel-admin.sh`:

```bash
# Sanity check: who am I (via JWT bearer → access token)
./scripts/zitadel-admin.sh whoami

# Create another service user
./scripts/zitadel-admin.sh create-machine-user my-new-service

# Grant it a role
./scripts/zitadel-admin.sh grant-iam-role <user_id> IAM_LOGIN_USER

# Mint a PAT
./scripts/zitadel-admin.sh create-pat <user_id> 180    # 180-day expiry

# Or mint + auto-install into platform-core-secrets + rollout
./scripts/zitadel-admin.sh save-pat-to-secret <user_id> SOME_SECRET_KEY
```

## Rotating the admin password

After bootstrap, **rotate `zitadel-admin@zitadel.auth.lurus.cn`'s password** so
the short-lived bootstrap creds aren't sitting in k8s. The machine-key JSON is
now authoritative; the original admin password is only useful for breakglass
console access.

```bash
# Update k8s secret
kubectl edit secret zitadel-secrets -n lurus-platform
# Change admin-password → new value (base64-encoded)
# Then log back into auth.lurus.cn once to set the password to match.
```

## What if I lose the JSON?

The private key is irretrievable after download — but creating a new key for
the same service user is painless:

1. Console → Service Users → `platform-admin-sa` → Keys → New → JSON
2. Re-run `./scripts/bootstrap-zitadel.sh <new.json>`
3. Console → Service Users → `platform-admin-sa` → Keys → delete the old key

## Why not a PAT directly?

A machine key:
- **Never expires** (the PATs derived from it do — rotatable via one command).
- Lets any tool that can sign a JWT mint access tokens; no long-lived bearer
  token sits in a secret store.
- Supports running the admin flow from CI without extra secret-management.

A PAT:
- Is a long-lived bearer token; if leaked, the attacker has full API access
  until rotation.
- Is the right choice for stable service-to-service auth (which is why
  `platform-core` itself gets ONE derived PAT for the custom-login proxy).
