#!/usr/bin/env bash
# e2e handoff smoke test — requires a running SpecGate stack with seeded demo data.
# Usage:
#   SPECGATE_SERVER=http://localhost:8080 bash app/cli/test/e2e/handoff.sh
#
# The delivery mutation steps (delivery report + review) require at least one
# work item in "Ready" or "Handoff" phase from `specgate init --seed` or
# doc-registry `--seed-demo`.
set -euo pipefail

SPECGATE_SERVER="${SPECGATE_SERVER:-http://localhost:8080}"
export SPECGATE_SERVER

echo "--- specgate doctor"
specgate doctor --no-input

echo "--- specgate status"
specgate status --json | jq -e '.ok == true' > /dev/null

echo "--- list ready work items"
WORK_REF="$(specgate work list --ready --json | jq -r '.data.items[0].change_request_key // empty')"
if [ -z "$WORK_REF" ]; then
  echo "SKIP: no ready work items found (run specgate init --seed or doc-registry --seed-demo first)" >&2
  exit 0
fi
echo "    work_ref=$WORK_REF"

echo "--- specgate work show"
specgate work show "$WORK_REF" --json | jq -e '.ok == true' > /dev/null

echo "--- specgate work context"
specgate work context "$WORK_REF" --json | jq -e '(.data.markdown | length) > 0' > /dev/null

echo "--- specgate gates run (dry verify)"
specgate gates run "$WORK_REF" --json | jq -e '.ok == true' > /dev/null

echo "--- specgate delivery status"
specgate delivery status "$WORK_REF" --json | jq -e '.ok == true' > /dev/null

echo "OK: handoff smoke passed for $WORK_REF"
