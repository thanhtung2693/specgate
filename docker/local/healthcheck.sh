#!/usr/bin/env bash
set -euo pipefail

curl --fail --silent --show-error --max-time 4 http://127.0.0.1:9090/healthz >/dev/null
