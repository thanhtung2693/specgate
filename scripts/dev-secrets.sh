#!/usr/bin/env bash
# Create the private environment consumed by the contributor appliance.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/deploy/local/specgate.env"
EXAMPLE="$ROOT/deploy/local/specgate.env.example"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$EXAMPLE" "$ENV_FILE"
  chmod 0600 "$ENV_FILE"
  echo "Created deploy/local/specgate.env."
fi

current="$(grep -E '^SETTINGS_ENCRYPTION_KEY=' "$ENV_FILE" | head -n1 | cut -d= -f2- || true)"
if [[ -n "${current//[[:space:]]/}" ]]; then
  chmod 0600 "$ENV_FILE"
  echo "SETTINGS_ENCRYPTION_KEY already set; leaving it unchanged."
  exit 0
fi

if command -v openssl >/dev/null 2>&1; then
  key="$(openssl rand -hex 32)"
else
  key="$(head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
fi

tmp="$(mktemp)"
awk -v key="$key" '
  /^SETTINGS_ENCRYPTION_KEY=/ { print "SETTINGS_ENCRYPTION_KEY=" key; found=1; next }
  { print }
  END { if (!found) print "SETTINGS_ENCRYPTION_KEY=" key }
' "$ENV_FILE" >"$tmp"
chmod 0600 "$tmp"
mv "$tmp" "$ENV_FILE"
echo "Generated SETTINGS_ENCRYPTION_KEY in deploy/local/specgate.env."
