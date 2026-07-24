#!/usr/bin/env bash
# Prepare deploy/local/.env for the contributor appliance without touching an
# existing selection. Only the appliance gateway port is published.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/deploy/local/.env"
EXAMPLE="$ROOT/deploy/local/.env.example"
NON_INTERACTIVE=false
[[ "${1:-}" == "--non-interactive" ]] && NON_INTERACTIVE=true

if [[ -f "$ENV_FILE" ]]; then
  echo "deploy/local/.env already exists; leaving it unchanged."
  exit 0
fi

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$port" -sTCP:LISTEN -P -n >/dev/null 2>&1
  else
    nc -z 127.0.0.1 "$port" >/dev/null 2>&1
  fi
}

port=3000
if port_in_use "$port"; then
  if $NON_INTERACTIVE; then
    echo "ERROR: Port 3000 is in use. Create deploy/local/.env with another SPECGATE_PORT." >&2
    exit 1
  fi
  while port_in_use "$port"; do
    printf "Port %s is in use. Choose another [%s]: " "$port" "$((port + 1))" >&2
    read -r selected
    port="${selected:-$((port + 1))}"
  done
fi

cp "$EXAMPLE" "$ENV_FILE"
chmod 0600 "$ENV_FILE"
sed -i.bak \
  -e 's/^SPECGATE_VERSION=.*/SPECGATE_VERSION=dev/' \
  -e "s/^SPECGATE_PORT=.*/SPECGATE_PORT=$port/" \
  -e "s|^APP_BASE_URL=.*|APP_BASE_URL=http://localhost:$port|" \
  "$ENV_FILE"
rm -f "$ENV_FILE.bak"
echo "Created deploy/local/.env for port $port."
