#!/usr/bin/env bash
# e2e handoff smoke test — requires a running SpecGate stack.
# Usage:
#   SPECGATE_SERVER=http://localhost:3000/api/doc-registry bash app/cli/test/e2e/handoff.sh
#
# The script uses a temporary HOME, logs in as a disposable local user, creates
# a quick work item, verifies the handoff/readiness CLI path, and archives the
# item before exiting. It intentionally leaves no dependency on seeded demo data.
set -euo pipefail

SPECGATE_SERVER="${SPECGATE_SERVER:-http://localhost:3000/api/doc-registry}"
export SPECGATE_SERVER

RUN_ID="${SPECGATE_E2E_RUN_ID:-$(date +%s)}"
USER_RUN_ID="${RUN_ID//[^[:alnum:]]/-}"
USER_RUN_ID="${USER_RUN_ID:0:24}"
REMOVE_SMOKE_HOME=0
REMOVE_SMOKE_WORKDIR=0
if [ -z "${SMOKE_HOME:-}" ]; then
  SMOKE_HOME="$(mktemp -d /tmp/specgate-e2e-home.XXXXXX)"
  REMOVE_SMOKE_HOME=1
fi
if [ -z "${SMOKE_WORKDIR:-}" ]; then
  SMOKE_WORKDIR="$(mktemp -d /tmp/specgate-e2e-work.XXXXXX)"
  REMOVE_SMOKE_WORKDIR=1
fi
export HOME="$SMOKE_HOME"

WORK_REF=""

cleanup() {
  if [ -n "$WORK_REF" ]; then
    specgate work archive "$WORK_REF" --reason "e2e smoke cleanup" --json --no-input > /dev/null || true
  fi
  if [ "$REMOVE_SMOKE_HOME" -eq 1 ]; then
    rm -rf "$SMOKE_HOME"
  fi
  if [ "$REMOVE_SMOKE_WORKDIR" -eq 1 ]; then
    rm -rf "$SMOKE_WORKDIR"
  fi
}
trap cleanup EXIT

echo "--- specgate doctor"
specgate doctor --no-input

echo "--- login disposable user/workspace"
specgate user login \
  --workspace "E2E Smoke Workspace $RUN_ID" \
  --display-name "E2E Smoke User" \
  --username "e2e-smoke-$USER_RUN_ID" \
  --email "e2e-smoke-$USER_RUN_ID@example.com" \
  --json --no-input | jq -e '.ok == true' > /dev/null

echo "--- plugin install/doctor in temporary HOME"
specgate plugins install --agent all --json --no-input | jq -e '.ok == true' > /dev/null
specgate plugins doctor --agent all --json --no-input | jq -e '.ok == true' > /dev/null

echo "--- specgate status"
specgate status --json | jq -e '.ok == true' > /dev/null

echo "--- create disposable quick work"
WORK_REF="$(
  specgate work create-quick "E2E smoke handoff $RUN_ID" \
    --description "Disposable CLI handoff smoke for a new local user." \
    --ac "The smoke workflow can fetch a Context Pack." \
    --ac "The smoke workflow can persist delivery evidence." \
    --json --no-input | jq -r '.data.change_request_key'
)"
echo "    work_ref=$WORK_REF"

echo "--- specgate work show"
specgate work show "$WORK_REF" --json | jq -e '.ok == true' > /dev/null

echo "--- quick work is pickup-ready"
specgate work list --phase ready --json \
  | jq -e --arg ref "$WORK_REF" '.ok == true and any(.data.items[]; .key == $ref and .phase == "Ready")' > /dev/null

echo "--- specgate work context"
specgate work context "$WORK_REF" --json | jq -e '(.data.markdown | length) > 0' > /dev/null

echo "--- specgate gates run (dry verify)"
specgate gates run "$WORK_REF" --json | jq -e '.ok == true' > /dev/null

echo "--- scaffold and submit delivery evidence"
COMPLETION="$SMOKE_WORKDIR/completion.json"
specgate delivery report "$WORK_REF" --init "$COMPLETION" --force --json --no-input | jq -e '.ok == true' > /dev/null
jq \
  --arg summary "E2E smoke completed the handoff path." \
  --arg agent "SpecGate CLI E2E" \
  --arg evidence "app/cli/test/e2e/handoff.sh" \
  '.summary = $summary
    | .agent.name = $agent
    | .affected_files = [$evidence]
    | .checks = [{"name":"tests","command":"test -f app/cli/test/e2e/handoff.sh","status":"pass","detail":"handoff smoke commands completed"}]
    | .criteria |= map(.claim = "satisfied" | .evidence = {"kind":"command","path":$evidence})' \
  "$COMPLETION" > "$COMPLETION.tmp"
mv "$COMPLETION.tmp" "$COMPLETION"
SUBMIT_CODE=0
SUBMIT_JSON="$(specgate delivery submit "$WORK_REF" --file "$COMPLETION" --json --no-input)" || SUBMIT_CODE=$?
if [ "$SUBMIT_CODE" -ne 0 ] && [ "$SUBMIT_CODE" -ne 1 ]; then
  printf '%s\n' "$SUBMIT_JSON" >&2
  echo "delivery submit failed with exit $SUBMIT_CODE" >&2
  exit "$SUBMIT_CODE"
fi
printf '%s\n' "$SUBMIT_JSON" \
  | jq -e '.ok == true and (.data.status.verdict | length) > 0' > /dev/null

echo "--- specgate delivery status"
STATUS_CODE=0
STATUS_JSON="$(specgate delivery status "$WORK_REF" --detail --json)" || STATUS_CODE=$?
if [ "$STATUS_CODE" -ne 0 ] && [ "$STATUS_CODE" -ne 1 ]; then
  printf '%s\n' "$STATUS_JSON" >&2
  echo "delivery status failed with exit $STATUS_CODE" >&2
  exit "$STATUS_CODE"
fi
printf '%s\n' "$STATUS_JSON" \
  | jq -e '.ok == true and (.data.verdict | length) > 0' > /dev/null

echo "OK: handoff smoke passed for $WORK_REF"
