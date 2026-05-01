#!/usr/bin/env bash
# restore-from-backup.sh — restore lurus-platform `identity` DB from a pg_dump
# produced by the daily-pg-dump CronJob (deploy/k8s/cronjobs/pg-backup.yaml).
#
# Default-off semantics: this script does NOT auto-run; an operator runs it
# manually after a corruption / accidental DELETE / migration disaster.
#
# Examples:
#   ./scripts/restore-from-backup.sh --latest --dry-run
#   ./scripts/restore-from-backup.sh lurus-identity-20260501-020000.dump
#   ./scripts/restore-from-backup.sh --latest --yes        # non-interactive
#
# Pre-flight: kubectl context must point at the R6 cluster (database ns
# contains lurus-pg-1). The PVC `lurus-pg-backup` must be mounted; the
# script uses an ephemeral debug pod-style exec into lurus-pg-1 itself
# only IF the dump was streamed to its own PVC; otherwise we copy via
# kubectl cp from the backup PVC by spawning a busybox helper.

set -euo pipefail

NS="database"
PG_POD="lurus-pg-1"
DB_NAME="identity"
BACKUP_DIR="/backups"

DRY_RUN=0
ASSUME_YES=0
DUMP_ARG=""

usage() {
  cat <<'EOF'
Usage: restore-from-backup.sh <dump-filename | --latest> [--dry-run] [--yes]

Args:
  <dump-filename>   Filename of dump in /backups (inside backup PVC)
  --latest          Pick newest lurus-identity-*.dump in /backups
  --dry-run         Print intended commands without executing
  --yes             Skip interactive confirmation
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --yes|-y)  ASSUME_YES=1; shift ;;
    --latest)  DUMP_ARG="--latest"; shift ;;
    -h|--help) usage; exit 0 ;;
    -*)        echo "unknown flag: $1" >&2; usage; exit 2 ;;
    *)         DUMP_ARG="$1"; shift ;;
  esac
done

if [ -z "${DUMP_ARG}" ]; then
  usage
  exit 2
fi

run() {
  if [ "${DRY_RUN}" = "1" ]; then
    echo "+ $*"
  else
    echo "+ $*"
    "$@"
  fi
}

# 1. Validate kubectl access to namespace
echo "==> verifying kubectl access to namespace ${NS}"
if ! kubectl get ns "${NS}" >/dev/null 2>&1; then
  echo "ERROR: cannot access namespace ${NS} — check kubectl context" >&2
  exit 1
fi
if ! kubectl -n "${NS}" get pod "${PG_POD}" >/dev/null 2>&1; then
  echo "ERROR: pod ${PG_POD} not found in ns ${NS}" >&2
  exit 1
fi

# Helper to run a command inside a backup-PVC-attached helper pod.
# For simplicity we exec into a temp pod that mounts the PVC.
HELPER_POD="lurus-restore-helper-$$"
cleanup_helper() {
  kubectl -n "${NS}" delete pod "${HELPER_POD}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
}
trap cleanup_helper EXIT

start_helper() {
  cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: ${HELPER_POD}
  namespace: ${NS}
  labels:
    app.kubernetes.io/component: restore-helper
spec:
  restartPolicy: Never
  containers:
    - name: helper
      image: postgres:16-alpine
      command: ["sleep", "3600"]
      volumeMounts:
        - name: backup-vol
          mountPath: /backups
  volumes:
    - name: backup-vol
      persistentVolumeClaim:
        claimName: lurus-pg-backup
EOF
  echo "==> waiting for helper pod ${HELPER_POD} to be ready"
  kubectl -n "${NS}" wait --for=condition=Ready "pod/${HELPER_POD}" --timeout=120s >/dev/null
}

# 2. Resolve dump filename
if [ "${DUMP_ARG}" = "--latest" ]; then
  if [ "${DRY_RUN}" = "1" ]; then
    DUMP_FILE="lurus-identity-DRYRUN.dump"
    echo "==> [dry-run] would resolve --latest via helper pod"
  else
    start_helper
    DUMP_FILE=$(kubectl -n "${NS}" exec "${HELPER_POD}" -- sh -c \
      "ls -1t ${BACKUP_DIR}/lurus-identity-*.dump 2>/dev/null | head -1 | xargs -n1 basename" \
      || true)
    if [ -z "${DUMP_FILE}" ]; then
      echo "ERROR: no dump files found in ${BACKUP_DIR}" >&2
      exit 1
    fi
    echo "==> latest dump: ${DUMP_FILE}"
  fi
else
  DUMP_FILE="${DUMP_ARG}"
  if [ "${DRY_RUN}" != "1" ]; then
    start_helper
    if ! kubectl -n "${NS}" exec "${HELPER_POD}" -- test -f "${BACKUP_DIR}/${DUMP_FILE}"; then
      echo "ERROR: dump ${BACKUP_DIR}/${DUMP_FILE} not found in PVC" >&2
      exit 1
    fi
  fi
fi

# 3. Confirm with user
if [ "${ASSUME_YES}" != "1" ] && [ "${DRY_RUN}" != "1" ]; then
  echo
  echo "ABOUT TO RESTORE database '${DB_NAME}' on pod ${PG_POD} (ns ${NS})"
  echo "  source dump : ${BACKUP_DIR}/${DUMP_FILE}"
  echo "  current DB will be RENAMED (not dropped) to identity_pre_restore_<epoch>"
  printf "Proceed? [y/N] "
  read -r CONFIRM
  case "${CONFIRM}" in
    y|Y|yes|YES) ;;
    *) echo "aborted"; exit 1 ;;
  esac
fi

# 4. Pre-restore safeguard: rename current DB
EPOCH=$(date +%s)
PRESERVED_DB="identity_pre_restore_${EPOCH}"
echo "==> renaming current ${DB_NAME} -> ${PRESERVED_DB} (rollback-safe)"
PSQL_RENAME_SQL="
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
 WHERE datname='${DB_NAME}' AND pid <> pg_backend_pid();
ALTER DATABASE ${DB_NAME} RENAME TO ${PRESERVED_DB};
CREATE DATABASE ${DB_NAME};
"
if [ "${DRY_RUN}" = "1" ]; then
  echo "+ kubectl -n ${NS} exec ${PG_POD} -- psql -U postgres -d postgres -c '<rename + recreate ${DB_NAME}>'"
else
  kubectl -n "${NS}" exec -i "${PG_POD}" -- \
    psql -U postgres -d postgres -v ON_ERROR_STOP=1 <<EOF
${PSQL_RENAME_SQL}
EOF
fi

# 5. Run pg_restore
echo "==> running pg_restore"
if [ "${DRY_RUN}" = "1" ]; then
  echo "+ kubectl -n ${NS} exec ${HELPER_POD} -- pg_restore --clean --if-exists --no-owner --no-privileges \\"
  echo "    -h lurus-pg-1.${NS}.svc.cluster.local -U postgres -d ${DB_NAME} ${BACKUP_DIR}/${DUMP_FILE}"
else
  # PGPASSWORD must be supplied. Pull from secret in cluster.
  PG_PW=$(kubectl -n "${NS}" get secret lurus-pg-superuser \
    -o jsonpath='{.data.postgres-password}' | base64 -d)
  kubectl -n "${NS}" exec "${HELPER_POD}" -- env "PGPASSWORD=${PG_PW}" \
    pg_restore --clean --if-exists --no-owner --no-privileges \
      -h "lurus-pg-1.${NS}.svc.cluster.local" \
      -U postgres -d "${DB_NAME}" \
      "${BACKUP_DIR}/${DUMP_FILE}" || {
        echo
        echo "WARN: pg_restore exited non-zero; '700+ errors ignored on restore' is normal"
        echo "      (per infra-ops skill §5.2). Continuing to row-count verification."
      }
fi

# 6. Print row counts of headline tables
echo "==> verification: row counts of headline tables"
COUNT_SQL="
SELECT 'identity.accounts' AS table, COUNT(*) AS rows FROM identity.accounts
UNION ALL SELECT 'billing.payment_orders',  COUNT(*) FROM billing.payment_orders
UNION ALL SELECT 'billing.subscriptions',   COUNT(*) FROM billing.subscriptions
UNION ALL SELECT 'module.hook_failures',    COUNT(*) FROM module.hook_failures;
"
if [ "${DRY_RUN}" = "1" ]; then
  echo "+ kubectl -n ${NS} exec ${PG_POD} -- psql -U postgres -d ${DB_NAME} -c '<row counts>'"
else
  kubectl -n "${NS}" exec -i "${PG_POD}" -- \
    psql -U postgres -d "${DB_NAME}" -c "${COUNT_SQL}" || true
fi

# 7. Print rollback hint
cat <<EOF

==> restore complete (or dry-run preview complete).
==> If restore looks WRONG, rollback with:

    kubectl -n ${NS} exec -i ${PG_POD} -- psql -U postgres -d postgres <<'SQL'
    SELECT pg_terminate_backend(pid) FROM pg_stat_activity
     WHERE datname='${DB_NAME}' AND pid <> pg_backend_pid();
    ALTER DATABASE ${DB_NAME} RENAME TO identity_failed_restore;
    ALTER DATABASE ${PRESERVED_DB} RENAME TO ${DB_NAME};
    SQL

==> Once verified, the preserved DB '${PRESERVED_DB}' can be dropped:
    DROP DATABASE ${PRESERVED_DB};

EOF
