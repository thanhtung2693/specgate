#!/usr/bin/env bash
set -euo pipefail

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
chown -R specgate:specgate /data/diagnostics /run/specgate
chown specgate:specgate /tmp/specgate-nginx
chown agents:agents /tmp/specgate-agents
chmod 0700 /data/postgres

if [[ ! "${SETTINGS_ENCRYPTION_KEY:-}" =~ ^[[:xdigit:]]{64}$ ]]; then
  echo "[entrypoint] SETTINGS_ENCRYPTION_KEY is required and must be exactly 64 hexadecimal characters" >&2
  exit 1
fi

exec /init
