#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
# v0.1.4 predates enhanced stores. v0.2.6 supports them and predates transfer.
baseline=aeea1ca5f2c7ce9d1450148bf54b7ca137eea02f
pre_transfer=2aa5c463ef61c58edb3b741e5ea8a1b2aa53c102

legacy_dir="$(mktemp -d "${TMPDIR:-/tmp}/specgate-legacy-cli.XXXXXX")"
trap 'rm -rf -- "$legacy_dir"' EXIT

build_baseline() {
  local revision="$1" build_dir="$legacy_dir/$2"
  if ! git cat-file -e "${revision}^{commit}" 2>/dev/null; then
    git fetch --no-tags --depth=1 origin "$revision"
  fi
  mkdir "$build_dir"
  git archive "$revision" app/cli | tar -x -C "$build_dir"
  (
    cd "$build_dir/app/cli"
    go build -o "$build_dir/specgate" ./cmd/specgate
  )
}

build_baseline "$baseline" pre-enhancement
build_baseline "$pre_transfer" pre-transfer

cd app/cli
SPECGATE_LEGACY_CLI="$legacy_dir/pre-enhancement/specgate" \
SPECGATE_PRE_TRANSFER_CLI="$legacy_dir/pre-transfer/specgate" \
  go test -race -count=1 ./internal/local -run '^Test(LegacyBinaryCannotMutateEnhancedStore|PreEnhancedBackupRestoresLegacyWritableSnapshot|GenerationStoreCompatibilityRefusesBeforeMutation)$' -v
