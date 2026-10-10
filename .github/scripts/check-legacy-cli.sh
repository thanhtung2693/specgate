#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
# v0.1.4 predates enhanced-store support. Pin the commit, not the movable tag.
baseline=aeea1ca5f2c7ce9d1450148bf54b7ca137eea02f
if ! git cat-file -e "${baseline}^{commit}" 2>/dev/null; then
  git fetch --no-tags --depth=1 origin "$baseline"
fi

legacy_dir="$(mktemp -d "${TMPDIR:-/tmp}/specgate-legacy-cli.XXXXXX")"
trap 'rm -rf -- "$legacy_dir"' EXIT
git archive "$baseline" app/cli | tar -x -C "$legacy_dir"
(
  cd "$legacy_dir/app/cli"
  go build -o "$legacy_dir/specgate" ./cmd/specgate
)

cd app/cli
SPECGATE_LEGACY_CLI="$legacy_dir/specgate" go test -race -count=1 ./internal/local -run '^Test(LegacyBinaryCannotMutateEnhancedStore|PreEnhancedBackupRestoresLegacyWritableSnapshot)$' -v
