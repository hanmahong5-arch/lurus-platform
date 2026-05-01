# Runbook: `Errors.App.NotFound` from Zitadel

## When to use this runbook

A user reports "登录后白页" / blank page after sign-in on a *.lurus.cn product, and the OIDC redirect lands on Zitadel returning `{"error":"invalid_request","error_description":"Errors.App.NotFound"}`. Most often this is a stale `client_id` after a `delete_oidc_app` QR-delegate flow whose 24h tombstone is still suppressing recreation.

## Diagnostic

1. Check live state — does Zitadel know the app, and is a tombstone active?

   ```bash
   curl -H "Authorization: Bearer $ADMIN_JWT" \
     https://identity.lurus.cn/admin/v1/apps \
     | jq '.apps[] | select(.name=="<app>") | {name, environments: [.environments[] | {env, zitadel_app_id, tombstoned}]}'
   ```

   - `zitadel_app_id` empty → the Zitadel app is gone.
   - `tombstoned: true` → recreation is blocked by the 24h Redis tombstone (`qr_app_tombstone:<app>:<env>`).

2. Pod logs — was a reconcile attempted, and did it skip due to tombstone?

   ```bash
   ssh root@100.122.83.20 \
     "kubectl logs -n lurus-platform deploy/platform-core --tail=200 | grep -E 'app_registry|tombstone'"
   ```

3. If neither step shows a clear cause, check `ZITADEL_SERVICE_ACCOUNT_PAT` validity and that the project still exists in Zitadel (rare in steady state but does occur on cold-start environments).

## Fix

```bash
# 1) Clear the tombstone (the 24h block on recreating <app>/<env>).
curl -X POST -H "Authorization: Bearer $ADMIN_JWT" \
  https://identity.lurus.cn/admin/v1/apps/<app>/<env>/clear-tombstone
# 200 {"cleared":true,"app":"<app>","env":"<env>","note":"…"}

# 2) Trigger an immediate reconcile pass (don't wait the ~5min tick).
curl -X POST -H "Authorization: Bearer $ADMIN_JWT" \
  https://identity.lurus.cn/admin/v1/apps/reconcile-now
# 200 {"reconciled":true,"note":"…"}
```

The reconciler creates the Zitadel app, writes the new `client_id` into the K8s Secret (`<app>-secrets-<env>`), and triggers a rolling restart of the consuming deployment.

If the platform-core admin endpoints are unreachable (cold start, ingress down), the manual fallback is:

```bash
ssh root@100.122.83.20 \
  "kubectl exec -n lurus-platform deploy/redis -- redis-cli DEL qr_app_tombstone:<app>:<env>"
# Then either wait ≤5min for the next tick or restart platform-core to
# trigger an immediate pass on boot:
ssh root@100.122.83.20 \
  "kubectl rollout restart -n lurus-platform deploy/platform-core"
```

## Verification

```bash
# zitadel_app_id should now be populated for the affected env.
curl -H "Authorization: Bearer $ADMIN_JWT" \
  https://identity.lurus.cn/admin/v1/apps \
  | jq '.apps[] | select(.name=="<app>") | .environments[] | {env, zitadel_app_id, tombstoned}'
```

Have the user retry login from a fresh browser context (clear the redirect cookie or open a private window). Login should succeed and the product should land on its post-login page rather than the OIDC error.

## If this doesn't fix it

- `reconcile-now` returned errors → grab the most recent reconcile logs and look for the actual upstream error:

  ```bash
  ssh root@100.122.83.20 \
    "kubectl logs -n lurus-platform deploy/platform-core --tail=200 | grep app_registry"
  ```

  Common causes: `ZITADEL_SERVICE_ACCOUNT_PAT` expired (rotate via Zitadel console), Zitadel project deleted (recreate then re-run), or a YAML schema drift in `apps.yaml` (compare against the last working commit).
- The K8s Secret didn't pick up the new value (verify `kubectl -n lurus-platform get secret <app>-secrets-<env> -o jsonpath='{.data.OIDC_CLIENT_ID}' | base64 -d`); a `kubectl rollout restart` of the consuming deployment forces it to re-mount.
- The user's browser cached the OIDC discovery doc; force a hard refresh / clear site data.

## Related

- `CLAUDE.md` → "Recovery — `Errors.App.NotFound` from Zitadel" (canonical short-form reference; this runbook is the long-form expansion).
- `infra-ops` skill §6.62 ghcr decision tree — useful when the issue actually turns out to be a stale image rather than a Zitadel app mismatch.
- `docs/runbooks/hook-dlq-pileup.md` — sibling runbook for the DLQ surface.
