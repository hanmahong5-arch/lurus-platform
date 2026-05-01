# Alert rules — design rationale

> **Status (2026-05-01)**: Rules MIGRATED to `deploy/k8s/observability/prometheus-rules.yaml`.
> Alertmanager deployed in `monitoring` namespace, currently silenced by default in dev mode
> (see `deploy/k8s/observability/README.md`). This file is RETAINED as the human-readable
> rationale doc; the live source of truth is the K8s manifest. New rules: add to BOTH this file
> AND the manifest (one is the rationale, the other is the runtime).

The hardening checklist convention remains:

> 新加 metric 必须同时加 alert rule（设计写到 `docs/observability/alerts.md`，
> 运行时规则同步到 `deploy/k8s/observability/prometheus-rules.yaml`）

---

## Hook DLQ — pending failures (P1-9, 2026-05-01)

**Symptom**: An async lifecycle hook (mail / notification / newapi_sync /
…) has been failing permanently for at least one user, retry exhausted,
and operator hasn't replayed.

```yaml
groups:
- name: lurus-platform.hook-dlq
  rules:
  - alert: HookDLQPendingNonZero
    expr: lurus_platform_hook_dlq_pending > 0
    for: 15m
    labels:
      severity: warning
      team: platform
    annotations:
      summary: "Hook DLQ has {{ $value }} pending failures"
      description: |
        At least one async lifecycle hook has permanently failed and
        landed in module.hook_failures. Inspect at
        /admin/v1/onboarding-failures and either replay (after fixing
        the upstream cause) or accept-and-resolve.
      runbook: "https://lurus-internal.example/runbooks/hook-dlq.md"

  - alert: HookDLQRapidGrowth
    expr: rate(lurus_platform_hook_outcomes_total{result="dlq"}[5m]) > 0.1
    for: 5m
    labels:
      severity: critical
      team: platform
    annotations:
      summary: "Hook failures landing in DLQ at >0.1/sec"
      description: |
        Hooks are failing fast enough to suggest a downstream outage
        (Stalwart down, NewAPI admin token revoked, notification
        rate-limited). Check {{ $labels.event }} / {{ $labels.hook }}
        upstream health before replaying.
```

**Why two rules**: `Pending > 0` for 15m is the slow-burn signal (single
broken account no one noticed); the rate alert catches an active
outage where *every* signup is now failing — needs immediate response,
replay won't help until upstream is fixed.

---

## newapi_sync — sustained errors (existing, unwired)

```yaml
- alert: NewAPISyncErrorRateHigh
  expr: rate(lurus_platform_newapi_sync_ops_total{result="error"}[5m]) > 0.05
  for: 10m
  labels:
    severity: warning
    team: platform
  annotations:
    summary: "newapi_sync error rate >0.05/sec on {{ $labels.op }}"
    description: |
      Sustained NewAPI errors. Check NEWAPI_INTERNAL_URL reachability
      and admin token validity. The reconcile cron will retry orphans
      every 5 min, but new accounts fail in real time until fixed.
```

---

## Credential age — rotation overdue (P2-5 framework, 2026-05-01)

**Symptom**: A platform-privileged credential (currently
`zitadel_pat` or `newapi_admin_token`) has gone past its rotation
limit. Soft = plan it; hard = do it today. Detection only — actual
rotation is still manual until the rotation worker ships, see
`docs/runbooks/credential-rotation.md`.

The metric is published by the daily `credential_age_worker`
(`internal/app/credential_age_worker.go`), which is **opt-in** via
`CRON_CRED_AGE_ENABLED=true`. With the worker disabled the gauge is
absent and these rules silently no-op (which is correct — alerts on
a missing metric would just be noise during the rollout).

```yaml
groups:
- name: lurus-platform.credential-age
  rules:
  - alert: CredentialAgeTooHighSoft
    expr: lurus_platform_credential_age_days > 90
    for: 1h
    labels:
      severity: warning
      team: platform
    annotations:
      summary: "Credential {{ $labels.name }} is {{ $value }} days old"
      description: |
        Soft rotation limit (90d) crossed. Plan rotation in next sprint.
        See docs/runbooks/credential-rotation.md.

  - alert: CredentialAgeTooHighHard
    expr: lurus_platform_credential_age_days > 180
    for: 1h
    labels:
      severity: critical
      team: platform
    annotations:
      summary: "Credential {{ $labels.name }} is {{ $value }} days old (PAST HARD LIMIT)"
      description: |
        Hard rotation limit (180d) crossed. Rotate today.
        See docs/runbooks/credential-rotation.md.
```

**Why two thresholds**: 90d gives one full sprint of advance notice
so rotation can be scheduled like normal work; 180d is the
"this is now an active risk" page that warrants pulling someone off
their current task.

---

## QR confirm latency (existing, unwired)

```yaml
- alert: QRConfirmLatencyP99High
  expr: histogram_quantile(0.99, rate(lurus_platform_qr_confirm_latency_seconds_bucket[5m])) > 2
  for: 10m
  labels:
    severity: warning
    team: platform
  annotations:
    summary: "QR confirm p99 > 2s on action {{ $labels.action }}"
    description: |
      Lutu APP scan→confirm flow is slow. Check Redis latency, Zitadel
      callback API responsiveness, and platform-core CPU.
```
