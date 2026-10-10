#!/usr/bin/env bash
set -euo pipefail

service=$1
case "$service" in postgres|doc-registry|agents|nginx|health) ;; *) exit 1 ;; esac
exit_code=${2:-unknown}
signal=${3:-0}
state_dir=/run/specgate/components
restart_dir=/run/specgate/restarts
failure_file=/data/diagnostics/last-failure.json

mkdir -p "${state_dir}" "${restart_dir}" "$(dirname "${failure_file}")"

for file in "${state_dir}/${service}" "${restart_dir}/${service}"; do
  if [[ -L "$file" ]] || { [[ -e "$file" ]] && [[ ! -f "$file" ]]; }; then
    echo "[supervisor] unsafe state file: $file" >&2
    exit 1
  fi
done

# `wantedup` distinguishes a planned s6 down/shutdown from any process exit,
# including an unexpected clean exit or SIGTERM while the service should run.
wanted_up=$(/command/s6-svstat -o wantedup "/run/service/${service}" 2>/dev/null || printf 'false\n')
if [[ "${wanted_up}" != true ]]; then
  printf 'stopped\n' >"${state_dir}/${service}"
  rm -f "${restart_dir}/${service}"
  exit 0
fi

count=0
if [[ -f "${restart_dir}/${service}" ]]; then
  count=$(cat "${restart_dir}/${service}")
fi
if [[ ! "$count" =~ ^[0-9]{1,6}$ ]]; then
  echo "[supervisor] invalid restart counter for $service" >&2
  exit 1
fi
count=$((10#$count + 1))
printf '%s\n' "${count}" >"${restart_dir}/${service}"
printf 'failed\n' >"${state_dir}/${service}"

reason="exit_code=${exit_code} signal=${signal}"
tmp=$(mktemp "${failure_file}.XXXXXX")
printf '{"component":"%s","reason":"%s","restart_count":%s,"time":"%s"}\n' \
  "${service}" "${reason}" "${count}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"${tmp}"
mv "${tmp}" "${failure_file}"

if ((count >= 5)); then
  echo "[supervisor] ${service} failed ${count} consecutive times; stopping appliance (${reason})" >&2
  kill -TERM 1
fi
