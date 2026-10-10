#!/usr/bin/env bash
set -euo pipefail

# Refuse unsafe persisted supervisor state without deleting operator data.
for dir in /data/diagnostics /run/specgate /run/specgate/components /run/specgate/restarts; do
  if [[ -L "$dir" ]] || { [[ -e "$dir" ]] && [[ ! -d "$dir" ]]; }; then
    echo "[entrypoint] unsafe supervisor state path: $dir" >&2
    exit 1
  fi
done

mkdir -p \
  /data/postgres \
  /data/registry/blobs \
  /data/diagnostics \
  /run/specgate \
  /run/specgate/components \
  /run/specgate/restarts \
  /tmp/specgate-agents \
  /tmp/specgate-nginx

# Keep ownership scoped to each component's directory. In particular, never
# recursively chown /data: PostgreSQL and Doc Registry do not share files.
chown postgres:postgres /data/postgres
chown -R specgate:specgate /data/registry
if [[ -n "$(find /data/diagnostics /run/specgate ! -type d ! -type f -print -quit)" ]]; then
  echo "[entrypoint] supervisor state contains an unsafe link or special file; inspect it before restarting" >&2
  exit 1
fi
chown -R root:root /data/diagnostics /run/specgate
find /data/diagnostics /run/specgate -type d -exec chmod 0755 {} +
find /data/diagnostics /run/specgate -type f -exec chmod go-w {} +
chown specgate:specgate /tmp/specgate-nginx
chown agents:agents /tmp/specgate-agents
chmod 0700 /data/postgres

if [[ ! "${SETTINGS_ENCRYPTION_KEY:-}" =~ ^[[:xdigit:]]{64}$ ]]; then
  echo "[entrypoint] SETTINGS_ENCRYPTION_KEY is required and must be exactly 64 hexadecimal characters" >&2
  exit 1
fi

exec /init
