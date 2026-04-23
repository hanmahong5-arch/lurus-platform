#!/usr/bin/env bash
# bootstrap-zitadel.sh — one-shot install of the Zitadel machine key into k8s,
# mint a login PAT, patch platform-core-secrets, and roll out the deployment.
#
# Prereq: you have downloaded a Zitadel Service Account key JSON from
# https://auth.lurus.cn (Service Users → <user> → Keys → Add → JSON).
# See docs/zitadel-bootstrap.md for the console clicks.
#
# Usage:
#   ./bootstrap-zitadel.sh /path/to/downloaded-sa.json
#
# What it does (idempotent):
#   1. Validates the JSON has a private key + userId + keyId.
#   2. Uploads it to k8s secret zitadel-machinekey (overwrites).
#   3. Uses zitadel-admin.sh to mint a new PAT for the SAME machine user
#      (so the app can proxy user passwords to Zitadel Session API v2).
#   4. Writes the PAT into platform-core-secrets[ZITADEL_SERVICE_ACCOUNT_PAT].
#   5. Triggers kubectl rollout restart of platform-core.
#   6. Waits for rollout + verifies /api/v1/auth/login returns JSON (not HTML).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SSH_HOST="${SSH_HOST:-root@100.98.57.55}"

json_path="${1:-}"
if [[ -z "$json_path" || ! -f "$json_path" ]]; then
    echo "Usage: $0 <path-to-zitadel-sa-json>"
    echo ""
    echo "Get the JSON from https://auth.lurus.cn:"
    echo "  Service Users → <name> → Keys → Add → Type=JSON → Download"
    echo ""
    echo "The JSON must contain {type, userId, keyId, key: <private-key-pem>}"
    exit 1
fi

for cmd in jq kubectl ssh curl base64; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "ERROR: '$cmd' not in PATH" >&2; exit 1; }
done

# ── 1. Validate JSON ──────────────────────────────────────────────────────

echo "→ Validating SA JSON..."
user_id=$(jq -r '.userId // empty' "$json_path")
key_id=$(jq -r '.keyId // empty' "$json_path")
private_key=$(jq -r '.key // empty' "$json_path")

if [[ -z "$user_id" ]]; then
    echo "ERROR: .userId missing from JSON. Is this really a Zitadel SA key?"
    exit 1
fi
if [[ -z "$private_key" || "$private_key" != *"PRIVATE KEY"* ]]; then
    echo "ERROR: .key must contain a PEM private key. Got: ${private_key:0:80}..."
    echo "Tip: in Zitadel console choose key Type=JSON (not public-only)."
    exit 1
fi
echo "  userId=$user_id keyId=$key_id"

# ── 2. Upload to k8s ──────────────────────────────────────────────────────

echo "→ Uploading JSON to secret zitadel-machinekey (namespace: lurus-platform)..."
# Stream over SSH so nothing is written to local disk beyond the original file.
b64=$(base64 -w0 < "$json_path")
ssh "$SSH_HOST" "kubectl create secret generic zitadel-machinekey \
    --from-literal=zitadel-admin-sa.json='$(base64 -d <<< "$b64")' \
    -n lurus-platform --dry-run=client -o yaml | kubectl apply -f -" >/dev/null

# ── 3. Mint a new PAT via the admin script ────────────────────────────────

echo "→ Minting a Zitadel PAT for custom-login proxy..."
ZITADEL_SA_JSON="$json_path" "$SCRIPT_DIR/zitadel-admin.sh" \
    save-pat-to-secret "$user_id" ZITADEL_SERVICE_ACCOUNT_PAT

# ── 4. Wait for rollout + verify ──────────────────────────────────────────

echo "→ Waiting for platform-core rollout..."
ssh "$SSH_HOST" "kubectl rollout status deployment platform-core -n lurus-platform --timeout=300s" || true

echo "→ Verifying /api/v1/auth/login returns JSON..."
for i in 1 2 3 4 5; do
    resp=$(curl -sS -o /tmp/bootstrap_probe -w "%{http_code}|%{content_type}" \
        -X POST https://identity.lurus.cn/api/v1/auth/login \
        -H 'Content-Type: application/json' \
        -d '{"identifier":"x","password":"y"}' 2>/dev/null || echo "0|err")
    ct="${resp#*|}"
    code="${resp%|*}"
    if [[ "$ct" == application/json* ]]; then
        echo "  HTTP $code | $ct ✓"
        head -c 400 /tmp/bootstrap_probe
        echo
        echo ""
        echo "✓ Bootstrap complete. Custom login is now active."
        echo "  Users can sign in at https://identity.lurus.cn/login"
        exit 0
    fi
    echo "  attempt $i: HTTP $code $ct (waiting 5s...)"
    sleep 5
done

echo "⚠ Login endpoint still not returning JSON after rollout. Check pod logs:"
echo "    ssh $SSH_HOST 'kubectl logs -n lurus-platform deploy/platform-core --tail=100'"
exit 1
