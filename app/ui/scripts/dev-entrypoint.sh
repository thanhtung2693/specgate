#!/bin/sh
set -eu

hash_file="node_modules/.specgate-package-lock.sha256"
current_hash="$(sha256sum package.json package-lock.json | sha256sum | awk '{print $1}')"

if [ ! -f "$hash_file" ] || [ "$(cat "$hash_file")" != "$current_hash" ]; then
  npm ci --no-audit --no-fund
  printf '%s' "$current_hash" > "$hash_file"
fi

exec npx vite --host 0.0.0.0 --port 80
