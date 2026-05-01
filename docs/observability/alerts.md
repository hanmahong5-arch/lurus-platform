# Alert rules — staging area

This file holds Prometheus alert rules that have been **declared in
code** (metric exists, instrumentation in place) but not yet wired to
Alertmanager. The hardening checklist convention is:

> 新加 metric 必须同时加 alert rule（暂存到 `docs/observability/alerts.md`，
> 下次集中接入 Alertmanager）

When wiring to Alertmanager, copy each rule into the platform's
`PrometheusRule` CR (or equivalent) and **delete the entry from this
file** with a CHANGELOG note.

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
