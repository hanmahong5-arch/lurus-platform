#!/usr/bin/env bash
# setup.sh — Clone Zitadel login UI source (sparse checkout) for local development.
# Run once after cloning this repo.
set -euo pipefail

UPSTREAM_DIR="upstream"
ZITADEL_REPO="https://github.com/zitadel/zitadel.git"
BRANCH="main"

if [ -d "$UPSTREAM_DIR/.git" ]; then
  echo "Upstream already cloned. Pulling latest..."
  git -C "$UPSTREAM_DIR" fetch origin "$BRANCH"
  git -C "$UPSTREAM_DIR" reset --hard "origin/$BRANCH"
  echo "Done."
  exit 0
fi

echo "Cloning Zitadel login UI (sparse, apps/login only)..."
git clone \
  --depth 1 \
  --filter=blob:none \
  --sparse \
  --branch "$BRANCH" \
  "$ZITADEL_REPO" \
  "$UPSTREAM_DIR"

git -C "$UPSTREAM_DIR" sparse-checkout set apps/login

echo ""
echo "Done. Next steps:"
echo "  cd upstream && pnpm install"
echo "  cp ../.env.local apps/login/.env.local  (fill in your credentials)"
echo "  pnpm --filter @zitadel/login dev"
