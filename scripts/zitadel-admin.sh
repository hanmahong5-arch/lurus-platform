#!/usr/bin/env bash
# zitadel-admin.sh — Zitadel Management API client using a machine-account JWT.
#
# Goal: fully API-driven admin ops (no clicking in the console) once the
#       machine account's JSON key is provisioned into k8s.
#
# Bootstrap (ONE-TIME, manual in Zitadel console):
#   1. Log in to https://auth.lurus.cn as the initial admin user.
#      (credentials: see k8s secret zitadel-secrets → admin-password)
#   2. Go to "Service Users" → create `platform-admin-sa`.
#   3. Grant IAM_OWNER (or IAM_ADMIN for least privilege).
#   4. Keys → "New" → "JSON" → download the JSON (contains the private key).
#   5. Put it into k8s:
#        kubectl create secret generic zitadel-machinekey \
#          --from-file=zitadel-admin-sa.json=./downloaded.json \
#          -n lurus-platform --dry-run=client -o yaml | \
#          kubectl apply -f -
#
# From then on this script (or any other JWT-bearer aware tool) can manage
# Zitadel programmatically.
#
# Usage:
#   ./zitadel-admin.sh whoami
#   ./zitadel-admin.sh create-machine-user <name>
#   ./zitadel-admin.sh grant-iam-role <user_id> <role>      # e.g. IAM_LOGIN_USER
#   ./zitadel-admin.sh create-pat <user_id> [days=365]
#   ./zitadel-admin.sh save-pat-to-secret <user_id> <k8s_secret_key>
#
# Dependencies: bash, curl, jq, openssl
# Optional env: ZITADEL_ISSUER (default https://auth.lurus.cn)
#               ZITADEL_SA_JSON (default pulls from k8s via SSH)

set -euo pipefail

# Requires a UNIX-ish shell with jq, curl, openssl, base64. Use a Linux host
# or WSL; Git Bash on Windows lacks jq by default.
for cmd in jq curl openssl base64; do
    command -v "$cmd" >/dev/null 2>&1 || {
        echo "ERROR: '$cmd' is required but not in PATH." >&2
        echo "  On Git Bash, install jq via 'pacman -S jq' (MSYS2) or run this script via:" >&2
        echo "    ssh root@100.98.57.55 'bash -s' < $0" >&2
        exit 1
    }
done

ZITADEL_ISSUER="${ZITADEL_ISSUER:-https://auth.lurus.cn}"
SSH_HOST="${SSH_HOST:-root@100.98.57.55}"

# ── SA JSON resolution ────────────────────────────────────────────────────
# Priority: $ZITADEL_SA_JSON env (path) → k8s secret over SSH

sa_json() {
    if [[ -n "${ZITADEL_SA_JSON:-}" && -f "$ZITADEL_SA_JSON" ]]; then
        cat "$ZITADEL_SA_JSON"
        return
    fi
    ssh -o ConnectTimeout=10 "$SSH_HOST" \
        "kubectl get secret zitadel-machinekey -n lurus-platform -o jsonpath='{.data.zitadel-admin-sa\.json}' | base64 -d"
}

# ── JWT bearer grant ──────────────────────────────────────────────────────

mint_jwt() {
    local sa_json_content user_id key_id private_key
    sa_json_content=$(sa_json)

    user_id=$(echo "$sa_json_content" | jq -r '.userId')
    key_id=$(echo "$sa_json_content" | jq -r '.keyId')
    private_key=$(echo "$sa_json_content" | jq -r '.key')

    if [[ "$user_id" == "null" || "$key_id" == "null" || "$private_key" == "null" ]]; then
        echo "ERROR: SA JSON is missing required fields (userId/keyId/key)." >&2
        echo "       The zitadel-machinekey secret currently holds only the public key." >&2
        echo "       Re-generate the machine-account key via Zitadel console (see script header)." >&2
        exit 1
    fi

    local now exp header payload signature
    now=$(date +%s)
    exp=$((now + 3600))

    header=$(printf '{"alg":"RS256","typ":"JWT","kid":"%s"}' "$key_id" | base64 -w0 | tr '+/' '-_' | tr -d '=')
    payload=$(jq -n \
        --arg iss "$user_id" \
        --arg sub "$user_id" \
        --arg aud "$ZITADEL_ISSUER" \
        --argjson iat "$now" \
        --argjson exp "$exp" \
        '{iss:$iss, sub:$sub, aud:$aud, iat:$iat, exp:$exp}' | base64 -w0 | tr '+/' '-_' | tr -d '=')

    # Write private key to a real temp file (process substitution doesn't
    # play nicely with openssl across all shells).
    local key_file
    key_file=$(mktemp)
    trap 'rm -f "$key_file"' RETURN
    printf '%s' "$private_key" > "$key_file"

    signature=$(printf '%s.%s' "$header" "$payload" | \
        openssl dgst -sha256 -sign "$key_file" | \
        base64 -w0 | tr '+/' '-_' | tr -d '=')

    printf '%s.%s.%s' "$header" "$payload" "$signature"
}

# ── Access token exchange ─────────────────────────────────────────────────

access_token() {
    local jwt
    jwt=$(mint_jwt)
    curl -fsS -X POST "$ZITADEL_ISSUER/oauth/v2/token" \
        -H 'Content-Type: application/x-www-form-urlencoded' \
        --data-urlencode 'grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer' \
        --data-urlencode 'scope=openid urn:zitadel:iam:org:project:id:zitadel:aud' \
        --data-urlencode "assertion=$jwt" | jq -r '.access_token'
}

# ── API helpers ───────────────────────────────────────────────────────────

mgmt_api() {
    local method="$1"; shift
    local path="$1"; shift
    local body="${1:-}"
    local tok
    tok=$(access_token)
    if [[ -n "$body" ]]; then
        curl -fsS -X "$method" "$ZITADEL_ISSUER/management/v1$path" \
            -H "Authorization: Bearer $tok" \
            -H 'Content-Type: application/json' \
            -d "$body"
    else
        curl -fsS -X "$method" "$ZITADEL_ISSUER/management/v1$path" \
            -H "Authorization: Bearer $tok"
    fi
}

# ── Commands ──────────────────────────────────────────────────────────────

cmd_whoami() {
    local tok
    tok=$(access_token)
    curl -fsS "$ZITADEL_ISSUER/oidc/v1/userinfo" \
        -H "Authorization: Bearer $tok" | jq .
}

cmd_create_machine_user() {
    local name="$1"
    mgmt_api POST /users/machine "$(jq -n --arg n "$name" \
        '{userName:$n, name:$n, description:"managed by zitadel-admin.sh", accessTokenType:"ACCESS_TOKEN_TYPE_BEARER"}')" | jq .
}

cmd_grant_iam_role() {
    local user_id="$1" role="$2"
    mgmt_api POST "/users/$user_id/grants" "$(jq -n --arg r "$role" \
        '{roleKeys:[$r]}')" | jq .
}

cmd_create_pat() {
    local user_id="$1"
    local days="${2:-365}"
    local exp
    exp=$(date -u -d "+${days} days" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || \
          date -u -v+${days}d +%Y-%m-%dT%H:%M:%SZ)
    mgmt_api POST "/users/$user_id/pats" "$(jq -n --arg e "$exp" \
        '{expirationDate:$e}')" | jq .
}

cmd_save_pat_to_secret() {
    local user_id="$1" secret_key="$2"
    local pat_response pat
    pat_response=$(cmd_create_pat "$user_id")
    pat=$(echo "$pat_response" | jq -r '.token')
    if [[ -z "$pat" || "$pat" == "null" ]]; then
        echo "ERROR: no token in PAT response: $pat_response" >&2
        exit 1
    fi

    local b64
    b64=$(printf '%s' "$pat" | base64 -w0)
    ssh "$SSH_HOST" "kubectl patch secret platform-core-secrets -n lurus-platform \
        --type merge -p '{\"data\":{\"$secret_key\":\"$b64\"}}'"
    echo "Saved PAT to platform-core-secrets[$secret_key]"
    echo "Rolling out platform-core to pick up new secret..."
    ssh "$SSH_HOST" "kubectl rollout restart deployment platform-core -n lurus-platform"
}

# ── Dispatch ──────────────────────────────────────────────────────────────

usage() {
    sed -n '4,30p' "$0"
    exit 1
}

cmd="${1:-}"; shift || true
case "$cmd" in
    whoami)              cmd_whoami "$@" ;;
    create-machine-user) cmd_create_machine_user "$@" ;;
    grant-iam-role)      cmd_grant_iam_role "$@" ;;
    create-pat)          cmd_create_pat "$@" ;;
    save-pat-to-secret)  cmd_save_pat_to_secret "$@" ;;
    *) usage ;;
esac
