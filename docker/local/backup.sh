#!/usr/bin/env bash
set -euo pipefail

workdir=$(mktemp -d)
services_stopped=false

restore_services() {
  if [[ "${services_stopped}" == true ]]; then
    /command/s6-svc -u /run/service/doc-registry /run/service/agents || true
  fi
  rm -rf "${workdir}"
}
trap restore_services EXIT

# Stop application writers while Postgres remains available. This makes the
# logical database dump and registry snapshot describe the same quiet state.
echo "[backup] quiescing Doc Registry and Agents" >&2
/command/s6-svc -wD -T 15000 -d /run/service/agents /run/service/doc-registry
services_stopped=true

for _ in $(seq 1 60); do
  if pg_isready -h 127.0.0.1 -p 5432 -U "${POSTGRES_USER:-docreg}" -d "${POSTGRES_DB:-docreg}" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! pg_isready -h 127.0.0.1 -p 5432 -U "${POSTGRES_USER:-docreg}" -d "${POSTGRES_DB:-docreg}" >/dev/null 2>&1; then
  echo "[backup] PostgreSQL did not become ready within 60 seconds" >&2
  exit 1
fi

echo "[backup] creating consistent PostgreSQL dump" >&2
PGPASSWORD="${POSTGRES_PASSWORD:-docreg}" pg_dump \
  --host=127.0.0.1 \
  --username="${POSTGRES_USER:-docreg}" \
  --dbname="${POSTGRES_DB:-docreg}" \
  --no-owner \
  --no-privileges \
  >"${workdir}/docreg.sql"

mkdir -p "${workdir}/registry"
if [[ -d /data/registry ]]; then
  cp -a /data/registry/. "${workdir}/registry/"
fi

echo "[backup] streaming archive" >&2
tar -C "${workdir}" -czf - docreg.sql registry
