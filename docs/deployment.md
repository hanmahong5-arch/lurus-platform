# Deployment & Rollback

Operational guide for `lurus-platform-core` deployments and emergency rollbacks.

## Image tag strategy

CI (see `.github/workflows/core.yaml`) pushes three tags for every master push:

| Tag | Mutable? | Intended consumer | Notes |
|-----|----------|-------------------|-------|
| `:main` | **Yes** | ArgoCD auto-sync | Always points at the newest master build. `deploy/k8s/base/deployment.yaml` references this tag so rollouts happen by simply pushing a new image. |
| `:main-<sha7>` | **No** | Emergency rollback / forensic debugging | The immutable anchor. Pin this in `deployment.yaml` when you need to lock a specific revision. Track L / RL2.2 requirement. |
| `:main-YYYYMMDD-<sha7>` | **No** | Legacy / human-friendly scripts that grep by date | Kept for backward compatibility; prefer the bare `:main-<sha7>` for new work. |

At container start the binary logs its own build SHA:

```
{"time":"...","level":"INFO","msg":"lurus-platform build","sha":"a3f8c12","built_at":"2026-04-24T07:22:11Z","env":"production"}
```

Use this log line to verify which revision actually booted, independent of the ArgoCD-applied tag.

## Normal release

1. Merge PR → master.
2. Core CI (`Core CI/CD` workflow) builds and pushes the three tags.
3. ArgoCD auto-sync picks up `:main` within ~3 minutes and rolls out.
4. Watch: `ssh root@100.98.57.55 "kubectl rollout status deploy/platform-core -n lurus-platform"`.

## Emergency rollback

If a freshly deployed revision is misbehaving:

1. Identify the last-known-good SHA from Git history or from the "lurus-platform build" log lines in Loki/Grafana.
2. Edit `deploy/k8s/base/deployment.yaml`:
   ```yaml
   spec:
     template:
       spec:
         containers:
           - name: platform-core
             image: ghcr.io/hanmahong5-arch/lurus-platform-core:main-<last-good-sha7>
   ```
3. Commit + push to master. ArgoCD reconciles the pinned tag within ~3 minutes.
4. Once the production fire is out, either:
   - revert the pin back to `:main` in a follow-up PR (standard path); or
   - keep the pin in place until a fix-forward commit lands on master and the new `:main` is verified in staging first.

### Why not `kubectl set image` or `kubectl patch`?

Don't. ArgoCD auto-sync will overwrite any direct `kubectl` change on its next reconciliation pass. **Every permanent change must go through Git.** See root `CLAUDE.md` → Deployment rules.

### Why `:main-<sha7>` instead of the dated tag?

The dated tag works, but `:main-<sha7>` is deterministic (one SHA → one tag), easier to correlate with `git log`, and matches the log field emitted at startup. Use it.

## Verifying a build artefact

From any machine with `ghcr.io` access:

```bash
docker pull ghcr.io/hanmahong5-arch/lurus-platform-core:main-<sha7>
docker inspect ghcr.io/hanmahong5-arch/lurus-platform-core:main-<sha7> | jq '.[0].RepoDigests'
```

The image digest is a strong integrity anchor — two tags pointing at the same digest are byte-identical. You can also pin by digest (`@sha256:...`) in the deployment manifest if you need absolute paranoia.

## Related

- Root `lurus.yaml` — cross-service image tag convention.
- `docs/dependency-matrix.md` — runtime dependency checklist.
- `docs/observability-standard.md` — log/metric conventions (the build SHA field is logged on every start).
