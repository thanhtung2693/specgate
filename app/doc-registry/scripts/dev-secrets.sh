#!/usr/bin/env bash
# dev-secrets.sh — generate SETTINGS_ENCRYPTION_KEY into .env if missing.
# Called automatically by `make run` and `make dev` (idempotent).
# For a full first-time setup run `make setup` instead.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/../.env"
EXAMPLE_FILE="${ENV_FILE}.example"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$EXAMPLE_FILE" "$ENV_FILE"
  echo "==> Created .env from .env.example"
fi

# Generate SETTINGS_ENCRYPTION_KEY when the line is present but empty.
if grep -q '^SETTINGS_ENCRYPTION_KEY=$' "$ENV_FILE" 2>/dev/null; then
  KEY=$(openssl rand -hex 32)
  sed -i.bak "s|^SETTINGS_ENCRYPTION_KEY=.*|SETTINGS_ENCRYPTION_KEY=${KEY}|" "$ENV_FILE"
  rm -f "${ENV_FILE}.bak"
  echo "==> Generated SETTINGS_ENCRYPTION_KEY"
fi
