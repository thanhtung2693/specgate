#!/usr/bin/env bash
# setup.sh — one-time dev setup: generates required secrets into .env.
# Run once after cloning or when starting fresh: make setup
# Idempotent — never overwrites a secret that is already set.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Delegate SETTINGS_ENCRYPTION_KEY to the lightweight per-run helper.
"${SCRIPT_DIR}/dev-secrets.sh"

echo ""
echo "==> Setup complete. Next: fill in POSTGRES_DSN, then run: make run"
