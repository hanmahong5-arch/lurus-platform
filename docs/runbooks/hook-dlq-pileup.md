# Runbook: hook DLQ pile-up

## When to use this runbook

`lurus_platform_hook_dlq_pending > 5` sustained for 15 minutes (the `HookDLQRapidGrowth` Prometheus alert) **or** `GET /sli` reports `hook_dlq_pending.current > 0` with a growing trend across consecutive scrapes. The hook DLQ wraps async lifecycle hooks (`OnAccountCreated`, `OnPlanChanged`, `OnCheckin`, etc.) — when an upstream like Stalwart, the notification service, or NewAPI is down, fan-out failures land in `module.hook_failures` rather than crashing the request path.

## Diagnostic

1. List currently-pending failures:

   ```bash
   curl -H "Authorization: Bearer $ADMIN_JWT" \
     https://identity.lurus.cn/admin/v1/onboarding-failures \
     | jq '.data[] | {id, event, hook_name, account_id, attempts, error}'
   ```

2. Group by `hook_name` to identify the broken upstream:

   - All `mail` → Stalwart is down or unreachable. Confirm with `ssh root@100.122.83.20 "kubectl get pods -n lurus-platform | grep stalwart"` and a probe of `STALWART_ADMIN_URL`.
   - All `notification` → notification service is down. Same shape, namespace `lurus-platform`, deploy `lurus-notification`.
   - All `newapi_sync` → NewAPI is down. See `docs/runbooks/newapi-down.md` and recover that first.
   - Mixed → look for a shared dependency (Redis, Postgres) that recently degraded.

3. Confirm via pod logs that hooks are tripping the DLQ surface (rather than panicking):

   ```bash
   ssh root@100.122.83.20 \
     "kubectl logs -n lurus-platform deploy/platform-core --tail=400 | grep 'module hook permanently failed'"
   ```

## Fix

Restore the upstream FIRST. Replaying into a still-broken upstream just inflates `attempts` and doesn't drain the queue.

1. Recover the upstream service (Stalwart / notification / NewAPI). Reuse `docs/runbooks/newapi-down.md` for the NewAPI case.
2. Once the upstream is green, replay each pending row. With ≤20 rows, do it serially to avoid load-spiking the just-recovered upstream:

   ```bash
   ids=$(curl -s -H "Authorization: Bearer $ADMIN_JWT" \
     https://identity.lurus.cn/admin/v1/onboarding-failures | jq -r '.data[].id')
   for id in $ids; do
     curl -X POST -H "Authorization: Bearer $ADMIN_JWT" \
       https://identity.lurus.cn/admin/v1/onboarding-failures/$id/replay
     echo
   done
   ```

3. Edge cases — these responses are correct, not errors:

   - `200 {"replayed":true,"skipped":true,"reason":"account_purged_since_failure"}` — the account was purged after the original failure; the row is stamped replayed and stops counting against `pending_depth`. Nothing to do.
   - `409 already_replayed` — someone already drained this row (or you re-ran the loop). Idempotent.
   - `501 replay_unsupported` — the event type can't be replayed (e.g. `reconciliation_issue`). Mark intent in the row's `error` column and accept; the next reconciliation cron will re-emit if the underlying issue persists.

## Verification

- `hook_dlq_pending` returns to 0 (visible at `GET /sli` and on the Prometheus dashboard).
- `lurus_platform_hook_outcomes_total{result="replay_succeeded"}` increased by approximately the number of rows you replayed.
- Spot-check one of the affected accounts in the affected upstream — e.g. for `mail`, the Stalwart admin UI now lists the mailbox.

## If this doesn't fix it

- A hook may have been **renamed** since the DLQ row was written (the row's `hook_name` is the registry key — see `internal/module/registry.go`). Renames orphan DLQ rows; the only way to drain them is to re-add the old name as an alias for one cycle. Check `git log -- internal/module/registry.go` and the `feat(platform): hook DLQ` commit for the convention.
- Upstream is "up" but rejecting: enable verbose logs (`LOG_LEVEL=debug`) and replay one row to capture the upstream's actual error in the row's `error` column.
- DLQ depth keeps climbing during replay → the upstream isn't actually healthy yet, or the per-call timeout is too short to complete a backlog burst.

## Related

- `CLAUDE.md` → "Hook DLQ" section (canonical short-form reference).
- `docs/observability/alerts.md` — `HookDLQRapidGrowth` rule.
- `docs/runbooks/newapi-down.md` — sibling runbook for the most common upstream failure that triggers `newapi_sync` DLQ rows.
- `docs/runbooks/errors-app-not-found.md` — when a hook can't run because the OIDC app it depends on was deleted.
