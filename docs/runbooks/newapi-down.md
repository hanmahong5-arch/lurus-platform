# Runbook: NewAPI down

## When to use this runbook

`GET /readyz` reports `degraded.newapi=true`, **or** users report LLM features broken (UI shows "AI service temporarily unavailable", `/api/v1/account/me/llm-token` returns 503 `newapi_unavailable`), **or** the `lurus_platform_newapi_sync_ops_total{result="error"}` counter is climbing. NewAPI is wired as a SOFT readiness check — its outage degrades LLM features but does not pull platform-core out of the Service endpoints.

## Diagnostic

1. NewAPI pod state:

   ```bash
   ssh root@100.122.83.20 "kubectl get pods -n newapi"
   ssh root@100.122.83.20 "kubectl describe pod -n newapi -l app=newapi | tail -40"
   ```

2. Connectivity from platform-core to `NEWAPI_INTERNAL_URL`:

   ```bash
   ssh root@100.122.83.20 \
     "kubectl exec -n lurus-platform deploy/platform-core -- \
        wget -O- -T 5 \$NEWAPI_INTERNAL_URL/health"
   ```

3. Auth credentials are still alive:

   ```bash
   # Admin token should be a usable bearer (>30 chars). 0 == unset.
   ssh root@100.122.83.20 \
     "kubectl exec -n lurus-platform deploy/platform-core -- \
        sh -c 'echo -n \$NEWAPI_ADMIN_ACCESS_TOKEN | wc -c'"

   # Service-account PAT (used for Zitadel + downstream NewAPI mgmt).
   ssh root@100.122.83.20 \
     "kubectl exec -n lurus-platform deploy/platform-core -- \
        sh -c 'echo -n \$ZITADEL_SERVICE_ACCOUNT_PAT | wc -c'"
   ```

4. Recent platform-core logs grouped on the sync module:

   ```bash
   ssh root@100.122.83.20 \
     "kubectl logs -n lurus-platform deploy/platform-core --tail=400 | grep newapi_sync"
   ```

## Fix

- **Pod down / crash-looping**:

  ```bash
  ssh root@100.122.83.20 "kubectl rollout restart deployment/newapi -n newapi"
  ssh root@100.122.83.20 "kubectl rollout status deployment/newapi -n newapi --timeout=180s"
  ```

- **Admin token expired or revoked**: rotate `NEWAPI_ADMIN_ACCESS_TOKEN` in NewAPI's admin UI, then update `platform-core-secrets`:

  ```bash
  # See platform-core-secrets in lurus.yaml; update via the secret-management
  # path (sealed-secrets / kubeseal). Then restart platform-core to pick up
  # the new value:
  ssh root@100.122.83.20 "kubectl rollout restart -n lurus-platform deploy/platform-core"
  ```

  TODO: link `docs/runbooks/credential-rotation.md` once that runbook exists.

- **`ZITADEL_SERVICE_ACCOUNT_PAT` expired**: same pattern — rotate in Zitadel console, update `platform-core-secrets`, restart.

While NewAPI is down, the platform should remain partially functional:

- Login / wallet / subscription / mail / billing all keep working — they don't touch NewAPI.
- LLM token endpoint returns 503 with `newapi_unavailable`. Products MUST surface a clear "AI service temporarily unavailable" message rather than a generic error or silent failure.
- `newapi_sync` hook DLQ will accumulate while NewAPI is down — that's expected. After recovery, the rows replay cleanly via `docs/runbooks/hook-dlq-pileup.md`. Don't replay them while NewAPI is still flaky.

## Verification

- `GET /readyz` returns `ready=true` and either omits `degraded.newapi` or reports `false`.
- `GET /api/v1/account/me/llm-token` returns 200 with a usable bearer for an authenticated test account.
- The `lurus_platform_newapi_sync_ops_total{result="success"}` counter resumes climbing while `result="error"` is flat.
- Hook DLQ for `newapi_sync` drains via the standard replay (see related runbook).

## If this doesn't fix it

- Per-call retry + circuit-breaker (P1-1, P1-2) absorbs transient flaps. If you're past those layers and still failing, NewAPI itself is the problem — check NewAPI server logs and DB.
- The `newapi_sync` reconcile cron retries account-creation for orphans on a 5-minute tick; nothing to invoke manually.
- Permanent loss / data corruption is a P0 incident — escalate to the platform owner. Revenue paths that depend on NewAPI (LLM products) need explicit user-facing communication; do NOT silently leave them broken.

## Related

- `CLAUDE.md` → "Cross-Service Dependencies" (canonical short-form reference for `newapi_sync` and the LLM token contract).
- `docs/平台硬化清单.md` → P1-1 (per-call retry) and P1-2 (circuit-breaker) — the two layers that handle transient outages without your intervention.
- `docs/runbooks/hook-dlq-pileup.md` — drain `newapi_sync` DLQ rows after recovery.
- `docs/runbooks/errors-app-not-found.md` — separate failure mode (OIDC app gone) that can present similarly to users.
