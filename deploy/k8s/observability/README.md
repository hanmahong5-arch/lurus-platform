# Observability — Alertmanager + PrometheusRule

This overlay ships Alertmanager (silenced by default) and the
`PrometheusRule` CR holding all lurus-platform alert rules.

## What this deploys

- **Alertmanager** (`prom/alertmanager:v0.27.0`) — single-replica,
  `emptyDir` storage, `monitoring` namespace, ClusterIP `:9093`.
  Default route is `silenced-default` (no receivers — alerts evaporate).
  Critical-severity alerts route to `feishu-critical`, which points at
  a PLACEHOLDER webhook URL until an operator replaces it.
- **`PrometheusRule` CR** `lurus-platform-alerts` — 4 rules in 3 groups:
  - `lurus-platform.hook-dlq`
    - `HookDLQPendingNonZero` (warn, 15m)
    - `HookDLQRapidGrowth` (critical, 5m)
  - `lurus-platform.newapi-sync`
    - `NewAPISyncErrorRateHigh` (warn, 10m)
  - `lurus-platform.qr-latency`
    - `QRConfirmLatencyP99High` (warn, 10m)

Rationale for each rule lives in `docs/observability/alerts.md`.

## Prerequisites

A working Prometheus Operator stack (`kube-prometheus-stack` or
equivalent) must already be running, providing:

- The `monitoring.coreos.com/v1` CRDs (notably `PrometheusRule`).
- A Prometheus instance configured to discover `PrometheusRule` objects
  in the `monitoring` namespace. The default `kube-prometheus-stack`
  install with `ruleSelector: {}` already does this.

The platform-core `ServiceMonitor` already lives at
`deploy/k8s/base/servicemonitor.yaml` — that is what feeds the metrics
these rules query.

## Apply

```bash
kubectl apply -k deploy/k8s/observability/
# or via ArgoCD: add this path as a separate Application targeting the
# `monitoring` namespace.
```

## Verify rules loaded

```bash
# Prometheus should log the rule file pickup. Substitute your prom
# pod name; for kube-prometheus-stack it's usually `prometheus-server`
# or `prometheus-kube-prometheus-prometheus-0`.
kubectl logs -n monitoring deploy/prometheus-server | grep -i "loading rules"

# Then evaluate:
kubectl exec -n monitoring deploy/prometheus-server -- \
  promtool query instant http://localhost:9090 'ALERTS'
# Expect either zero rows (no firing alerts) or rows for the alerts
# above — never an error about an unknown metric.
```

To verify Alertmanager itself:

```bash
kubectl port-forward -n monitoring svc/alertmanager 9093:9093
# Open http://localhost:9093 — the routing tree should show
# silenced-default as catch-all and feishu-critical for severity=critical.
```

## Wire feishu webhook (when going prod)

```bash
# 1) Edit the Secret and replace the PLACEHOLDER URL.
kubectl edit secret -n monitoring alertmanager-config
# In the alertmanager.yml stringData entry, change the line:
#   url: 'https://open.feishu.cn/open-apis/bot/v2/hook/PLACEHOLDER'
# to:
#   url: 'https://open.feishu.cn/open-apis/bot/v2/hook/<real-token>'

# 2) Restart Alertmanager so it re-reads the config.
kubectl rollout restart -n monitoring deploy/alertmanager
kubectl rollout status  -n monitoring deploy/alertmanager
```

## Why silenced by default in dev mode

The repo is in **dev mode** (see `CLAUDE.md` first section: "Mode:
DEV (not yet production-ready)"). We don't yet want to page anyone
on every blip while the system is being hardened — especially given
that some alert metrics (`hook_dlq_pending`, etc.) just landed and
their thresholds are still being calibrated. The silenced-default
posture lets operators see alerts in the Alertmanager UI without
generating false-positive pages.

## Promote to prod

When the prod-readiness gate (see `docs/平台硬化清单.md` P2) is
crossed, this manifest needs the following changes:

1. **Replace the PLACEHOLDER feishu URL** with a real bot URL (see
   above). Feishu is currently the only critical channel; add an
   email or PagerDuty receiver if a second channel is required.
2. **Remove `silenced-default` catch-all**. Replace the default route
   with `feishu-warn` (or equivalent) so warning-severity alerts
   actually fire. Keep the critical route on a separate, lower-noise
   channel.
3. **Add a P99 latency receiver** if QR confirm latency is treated as
   a customer-facing SLI rather than a pure ops alert.
4. **Bump replicas to ≥ 2** and switch `emptyDir` to a `PersistentVolumeClaim`
   so notification dedup state survives restarts.
5. **Add the runbook URLs** referenced in each alert annotation
   (currently `lurus-internal.example/runbooks/...` placeholders).
