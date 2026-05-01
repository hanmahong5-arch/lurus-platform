# Runbook — PG Backup & Restore (lurus-platform `identity` DB)

Status: dev mode, **DEFAULT OFF**. As of 2026-05-01 backups are NOT enabled in any environment. This runbook documents the path the operator follows once they flip the feature on.

Companion artefacts:
- `deploy/k8s/cronjobs/pg-backup.yaml` — daily pg_dump + weekly S3 upload (env-flag gated)
- `scripts/restore-from-backup.sh` — interactive restore with rollback safeguard

---

## When to use

Restore from a pg_dump dump is appropriate for:

1. **Data corruption inside `identity` DB** — rows obviously wrong, FK violations, ORM panics. Schema-level damage.
2. **Accidental destructive SQL** — `DELETE`/`UPDATE` ran without `WHERE`, `DROP TABLE`, bad migration. Want to roll back to last good dump.
3. **Migration disaster** — a forward migration left the DB in a half-applied state and the down-migration is missing or unsafe.
4. **R6 disk failure recovery** — the host PVC was lost; restore from S3 (BACKUP_S3_ENABLED was true at backup time).
5. **Restore drill** — monthly verification that dumps are usable. See *Test cadence* footer.

NOT for: cross-cluster failover, point-in-time recovery (we don't ship WAL archiving), restoring an individual table (use `pg_restore -t`).

---

## Prerequisites

- kubectl context points at the R6 cluster (`kubectl get ns database` returns OK and shows `lurus-pg-1` pod ready).
- The dump you want to use exists in the backup PVC. List candidates:
  ```bash
  kubectl -n database run -i --rm peek --image=alpine --restart=Never \
    --overrides='{"spec":{"volumes":[{"name":"b","persistentVolumeClaim":{"claimName":"lurus-pg-backup"}}],"containers":[{"name":"peek","image":"alpine","stdin":true,"tty":true,"volumeMounts":[{"name":"b","mountPath":"/backups"}],"command":["ls","-lh","/backups"]}]}}'
  ```
  Or the simpler way (the restore script's helper pod does this automatically — just run with `--latest --dry-run`).
- `BACKUP_ENABLED` was `true` when the dump was taken. Confirm:
  ```bash
  kubectl -n database get cm lurus-platform-backup-config -o yaml | grep BACKUP_ENABLED
  ```
  If the CM doesn't exist, no dumps were ever created — stop here.
- For S3-restored dumps: download the `.dump` file out of S3 onto the backup PVC first (use the helper pod pattern).
- The superuser credential `lurus-pg-superuser` Secret exists in `database` ns. The script reads `data.postgres-password` from it.

---

## Step-by-step restore

```bash
# 0. Sanity: point kubectl at R6
kubectl config current-context
kubectl -n database get pod lurus-pg-1

# 1. Dry-run first to confirm the script can resolve the dump
./scripts/restore-from-backup.sh --latest --dry-run

# Expected output (abbreviated):
#   ==> verifying kubectl access to namespace database
#   ==> [dry-run] would resolve --latest via helper pod
#   ABOUT TO RESTORE database 'identity' on pod lurus-pg-1 (ns database)
#   ...
#   + kubectl -n database exec helper -- pg_restore --clean --if-exists ...

# 2. Real run, interactive
./scripts/restore-from-backup.sh --latest

# 3. Or non-interactive (CI / scripted recovery)
./scripts/restore-from-backup.sh lurus-identity-20260501-020000.dump --yes
```

The script:
1. Spawns a helper pod that mounts the backup PVC.
2. Renames the live `identity` DB to `identity_pre_restore_<epoch>` (NOT dropped).
3. Creates a fresh empty `identity`.
4. Runs `pg_restore --clean --if-exists --no-owner --no-privileges` from the dump.
5. Prints headline row counts.
6. Prints the rollback command.

---

## Verification after restore

The script auto-prints row counts. They should be in the right order of magnitude vs. your last known good state:

```
       table          | rows
----------------------+------
 identity.accounts    |  ...
 billing.payment_orders | ...
 billing.subscriptions  | ...
 module.hook_failures   | ...
```

Then run two sanity SELECTs as the operator:

```bash
kubectl -n database exec -it lurus-pg-1 -- psql -U postgres -d identity -c "
  SELECT MAX(created_at) AS newest_account FROM identity.accounts;
"
# Expect: roughly the timestamp the dump was taken (NOT the current wall clock).
# If newest_account is hours/days behind 'now', that's expected — you restored an old dump.

kubectl -n database exec -it lurus-pg-1 -- psql -U postgres -d identity -c "
  SELECT COUNT(*) FILTER (WHERE status='succeeded') AS paid,
         COUNT(*) FILTER (WHERE status='pending')   AS pending
    FROM billing.payment_orders
   WHERE created_at >= NOW() - INTERVAL '7 days';
"
# Spot-check that recent revenue activity matches your memory.
```

If row counts are zero or wildly wrong, do NOT keep the restored DB live — see *Rollback*.

---

## Rollback

The script preserves the pre-restore DB. To switch back:

```bash
PRESERVED=identity_pre_restore_<epoch>   # printed by the script
kubectl -n database exec -i lurus-pg-1 -- psql -U postgres -d postgres <<SQL
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
 WHERE datname='identity' AND pid <> pg_backend_pid();
ALTER DATABASE identity RENAME TO identity_failed_restore;
ALTER DATABASE ${PRESERVED} RENAME TO identity;
SQL
```

Once you're confident the right state is live, drop the loser:

```bash
DROP DATABASE identity_failed_restore;       # or identity_pre_restore_<epoch>
```

---

## Troubleshooting

| Symptom | Cause / Fix |
|---|---|
| `pg_restore` prints "errors ignored on restore: 700+" | **Normal**, per `infra-ops` skill §5.2. Owner/privilege/extension lines fail because we use `--no-owner --no-privileges`. Verify with row counts, not exit code. |
| `permission denied for schema billing` after restore | Re-run the GRANT block from migration `021_grant_module_schema_perms.sql` (or whichever migration owns the equivalent grants for your env). |
| PVC not mounted on cron pod | `kubectl -n database describe pod <cron-pod>` — check Events. Usually StorageClass mismatch or `lurus-pg-backup` PVC not bound. Fix the PVC, then `kubectl -n database create job --from=cronjob/daily-pg-dump manual-test`. |
| `BACKUP_ENABLED=true` set but no dumps appear | Confirm CronJob exists (`kubectl -n database get cronjob`), check last run (`kubectl -n database describe cronjob daily-pg-dump`), inspect a recent Job's pod logs (`kubectl -n database logs job/daily-pg-dump-<...>`). Often: PG password env wrong, or PG host unreachable from the namespace. |
| S3 upload fails with `Unable to locate credentials` | The `lurus-platform-backup-s3` Secret is missing or doesn't contain `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_DEFAULT_REGION`. |
| Helper pod stuck Pending during script run | PVC `lurus-pg-backup` is `ReadWriteOnce` and already attached to the cron pod. Wait for the cron run to finish or run during a quiet window. |

---

## Out of scope

- **Cross-cluster failover** — covered by `dr-failover.md` (does NOT exist yet, 2026-05-01).
- **Point-in-time recovery (PITR)** — requires WAL archiving; not configured.
- **Per-table partial restore** — operator can adapt by adding `pg_restore -t <table>` manually; not scripted here.
- **NewAPI / NewHub / Memorus DBs** — this runbook covers the `identity` DB only. Other services own their own backup discipline.

---

## Test cadence

Recommended: monthly restore drill on a non-prod DB to verify dumps are usable.
**Today (2026-05-01) status: NEVER PERFORMED.**
