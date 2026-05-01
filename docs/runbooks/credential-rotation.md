# Runbook: credential rotation (Zitadel PAT / NewAPI admin token)

## When this fires

Prometheus alert `CredentialAgeTooHighSoft` (>90 days) or
`CredentialAgeTooHighHard` (>180 days) — see
`docs/observability/alerts.md`. Source metric:
`lurus_platform_credential_age_days{name="..."}`. Currently tracked
credentials:

| `name` | What it is | Lives in |
|--------|------------|----------|
| `zitadel_pat` | Zitadel service-account PAT used by custom login UI (`/api/v1/auth/login`) | env `ZITADEL_SERVICE_ACCOUNT_PAT` (Secret `platform-core-secrets`) |
| `newapi_admin_token` | NewAPI admin token used by `newapi_sync` to provision users / topup / issue LLM tokens | env `NEWAPI_ADMIN_ACCESS_TOKEN` (Secret `platform-core-secrets`) |

## Today's status — framework only

Actual rotation must be performed **manually** until the rotation
worker ships (tracked: `docs/平台硬化清单.md` P2-5). The credential
age tracker (this runbook's source of alerts) is only the *detection*
half. Rotation cadence:

- Soft (>90 days, WARN): plan rotation in next sprint.
- Hard (>180 days, ERROR): rotate today.

## Rotate `zitadel_pat`

1. **Generate a new PAT** in Zitadel admin UI:
   Service Users → `lurus-platform-core` (or whichever service user)
   → **Personal Access Tokens** → Create. Copy the token; it will not
   be shown again.
2. **Update `platform-core-secrets`** on the cluster:
   ```bash
   ssh root@100.122.83.20 \
     "kubectl -n lurus-platform get secret platform-core-secrets -o yaml" \
     > /tmp/secret.yaml
   # edit /tmp/secret.yaml: replace ZITADEL_SERVICE_ACCOUNT_PAT with
   # echo -n '<new-token>' | base64 -w0
   ssh root@100.122.83.20 "kubectl apply -f -" < /tmp/secret.yaml
   rm /tmp/secret.yaml
   ```
3. **Restart platform-core** so the new value is loaded:
   ```bash
   ssh root@100.122.83.20 \
     "kubectl -n lurus-platform rollout restart deploy/platform-core"
   ssh root@100.122.83.20 \
     "kubectl -n lurus-platform rollout status deploy/platform-core"
   ```
4. **Stamp the rotation timestamp** so the gauge resets to ~0:
   ```bash
   # Option A — via the future CLI flag (not yet implemented):
   # kubectl exec -n lurus-platform deploy/platform-core -- /app -mark-credential-rotated zitadel_pat

   # Option B — direct Redis SET (works today). Replace REDIS_ADDR /
   # REDIS_PASSWORD with values from platform-core-secrets:
   ssh root@100.122.83.20 \
     "kubectl -n messaging exec deploy/redis -- redis-cli SET cred:rotated:zitadel_pat $(date -u +%FT%TZ)"
   ```
5. **Revoke the old PAT** in the Zitadel admin UI. This is the step
   most often forgotten — leaving the old PAT live defeats the
   rotation. Revoke immediately after step 3 confirms the new token
   works.
6. **Verify**:
   - `curl -fsS https://identity.lurus.cn/healthz` returns 200.
   - The custom login flow (`POST /api/v1/auth/login`) succeeds with
     a test account.
   - Within one tick (default 24h, or restart pod to fire boot
     sample) the metric resets:
     `lurus_platform_credential_age_days{name="zitadel_pat"} < 1`.

## Rotate `newapi_admin_token`

Same flow, different upstream:

1. **Generate a new admin token** in the NewAPI admin UI
   (`https://newapi.lurus.cn/console`) → Settings → Access Tokens →
   Create. The user this token belongs to must have admin role.
2. **Update `platform-core-secrets`** — same procedure as above,
   replacing `NEWAPI_ADMIN_ACCESS_TOKEN`. Note the related env
   `NEWAPI_ADMIN_USER_ID` doesn't change.
3. **Restart platform-core** — same `rollout restart`.
4. **Stamp the rotation timestamp**:
   ```bash
   ssh root@100.122.83.20 \
     "kubectl -n messaging exec deploy/redis -- redis-cli SET cred:rotated:newapi_admin_token $(date -u +%FT%TZ)"
   ```
5. **Revoke the old admin token** in the NewAPI admin UI.
6. **Verify**:
   - `lurus_platform_newapi_sync_ops_total{result="success"}` is
     still incrementing (new signups still propagate).
   - `lurus_platform_credential_age_days{name="newapi_admin_token"} < 1`.

## What this runbook does NOT cover yet

- **Automated rotation.** No worker today hits the Zitadel mgmt API
  or NewAPI's user-key API to rotate without operator action. That's
  tracked as `docs/平台硬化清单.md` P2-5 and is the natural next
  iteration once this framework has lived in prod for one rotation
  cycle. Until it ships, every rotation is manual.
- **Token revocation rollback.** If the new token is broken (e.g.
  scope mismatch), the old token is still revoked at step 5 — there
  is no automatic rollback. Mitigation: only revoke after step 6's
  verification has run for a full request cycle.
- **Cross-cluster propagation.** This runbook assumes single-cluster
  deployment. If `platform-core` ever fans out to multiple K8s
  clusters, the secret update must be replicated everywhere before
  step 4 stamps "rotated" — otherwise stale replicas will still hold
  the old token but the gauge will say "fresh".

## Related

- `docs/observability/alerts.md` — `CredentialAgeTooHighSoft` /
  `CredentialAgeTooHighHard` alert rules.
- `docs/平台硬化清单.md` P2-5 — tracking ticket for automated
  rotation.
- `internal/app/credential_age_worker.go` — worker that emits the
  gauge and exposes `MarkRotated` for programmatic stamping.
