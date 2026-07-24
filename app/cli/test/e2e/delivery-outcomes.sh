#!/usr/bin/env bash
# e2e delivery outcome smoke test — requires a running SpecGate stack.
# Usage:
#   SPECGATE_SERVER=http://localhost:3000/api/doc-registry bash app/cli/test/e2e/delivery-outcomes.sh
#
# The script temporarily points the delivery reviewer at Anthropic without
# changing any provider secret. In the local e2e stack there is no Anthropic env
# key or saved setting, so the agents service takes its deterministic
# coding-agent-claim fallback. It restores model + auto-archive settings on exit.
set -euo pipefail

SPECGATE_SERVER="${SPECGATE_SERVER:-http://localhost:3000/api/doc-registry}"
export SPECGATE_SERVER

RUN_ID="${SPECGATE_E2E_RUN_ID:-$(date +%s)}"
USER_RUN_ID="${RUN_ID//[^[:alnum:]]/-}"
USER_RUN_ID="${USER_RUN_ID:0:24}"
REMOVE_SMOKE_HOME=0
REMOVE_SMOKE_WORKDIR=0
if [ -z "${SMOKE_HOME:-}" ]; then
  SMOKE_HOME="$(mktemp -d /tmp/specgate-delivery-e2e-home.XXXXXX)"
  REMOVE_SMOKE_HOME=1
fi
if [ -z "${SMOKE_WORKDIR:-}" ]; then
  SMOKE_WORKDIR="$(mktemp -d /tmp/specgate-delivery-e2e-work.XXXXXX)"
  REMOVE_SMOKE_WORKDIR=1
fi
export HOME="$SMOKE_HOME"

ORIGINAL_MODEL_PROVIDER=""
ORIGINAL_MODEL=""
ORIGINAL_AUTO_ARCHIVE="false"
SETTINGS_SNAPSHOT_READY=0

put_settings() {
  curl -sS -X PUT "$SPECGATE_SERVER/settings" \
    -H "content-type: application/json" \
    -d "$1" > /dev/null
}

restore_settings() {
  if [ "$SETTINGS_SNAPSHOT_READY" -ne 1 ]; then
    return
  fi
  put_settings "$(
    jq -n \
      --arg provider "$ORIGINAL_MODEL_PROVIDER" \
      --arg model "$ORIGINAL_MODEL" \
      --arg auto "$ORIGINAL_AUTO_ARCHIVE" \
      '{settings:{
        "governance.model_provider":$provider,
        "governance.model":$model,
        "governance.auto_archive_on_delivery_pass":$auto
      }}'
  )" || true
}

snapshot_settings() {
  local original_settings
  original_settings="$(curl -sS "$SPECGATE_SERVER/settings")"
  ORIGINAL_MODEL_PROVIDER="$(printf '%s' "$original_settings" | jq -r '.settings["governance.model_provider"] // ""')"
  ORIGINAL_MODEL="$(printf '%s' "$original_settings" | jq -r '.settings["governance.model"] // ""')"
  ORIGINAL_AUTO_ARCHIVE="$(printf '%s' "$original_settings" | jq -r '.settings["governance.auto_archive_on_delivery_pass"] // "false"')"
  SETTINGS_SNAPSHOT_READY=1
}

cleanup() {
  restore_settings
  if [ -n "${NEEDS_REVIEW_REF:-}" ]; then
    specgate work archive "$NEEDS_REVIEW_REF" --reason "e2e smoke cleanup" --json --no-input > /dev/null || true
  fi
  if [ -n "${AUTO_ARCHIVE_REF:-}" ]; then
    specgate work archive "$AUTO_ARCHIVE_REF" --reason "e2e smoke cleanup" --json --no-input > /dev/null || true
  fi
  if [ "$REMOVE_SMOKE_HOME" -eq 1 ]; then
    rm -rf "$SMOKE_HOME"
  fi
  if [ "$REMOVE_SMOKE_WORKDIR" -eq 1 ]; then
    rm -rf "$SMOKE_WORKDIR"
  fi
}
trap cleanup EXIT

force_deterministic_review_settings() {
  local auto_archive="$1"
  put_settings "$(
    jq -n --arg auto "$auto_archive" '{settings:{
      "governance.model_provider":"anthropic",
      "governance.model":"claude-sonnet-4-6",
      "governance.auto_archive_on_delivery_pass":$auto
    }}'
  )"
}

create_work() {
  local title="$1"
  if [ "${2:-}" = "bound-check" ]; then
    specgate work create-quick "$title" \
      --description "Disposable CLI delivery outcome smoke." \
      --ac "The delivery report cites a passing check @check:tests" \
      --json --no-input | jq -r '.data.change_request_key'
    return
  fi
  specgate work create-quick "$title" \
    --description "Disposable CLI delivery outcome smoke." \
    --ac "The delivery report cites a passing check." \
    --ac "The delivery report records concrete evidence." \
    --json --no-input | jq -r '.data.change_request_key'
}

scaffold_completion() {
  local ref="$1"
  local path="$2"
  specgate delivery report "$ref" --init "$path" --force --json --no-input \
    | jq -e '.ok == true' > /dev/null
}

echo "--- specgate doctor"
specgate doctor --no-input
snapshot_settings

echo "--- login disposable user/workspace"
specgate user login \
  --workspace "E2E Delivery Workspace $RUN_ID" \
  --display-name "E2E Delivery User" \
  --username "e2e-delivery-$USER_RUN_ID" \
  --email "e2e-delivery-$USER_RUN_ID@example.com" \
  --json --no-input | jq -e '.ok == true' > /dev/null

echo "--- needs_human_review fallback outcome"
force_deterministic_review_settings "false"
NEEDS_REVIEW_REF="$(create_work "E2E needs-review smoke $RUN_ID")"
echo "    work_ref=$NEEDS_REVIEW_REF"
NEEDS_REVIEW_REPORT="$SMOKE_WORKDIR/needs-review-completion.json"
scaffold_completion "$NEEDS_REVIEW_REF" "$NEEDS_REVIEW_REPORT"
jq \
  --arg agent "e2e-builder-$USER_RUN_ID" \
  '.agent.name = $agent
    | .summary = "Partial evidence only."
    | .checks = [{"name":"tests","command":"test -f app/cli/test/e2e/delivery-outcomes.sh","status":"pass","detail":"one check ran"}]
    | .criteria |= (.[0].claim = "satisfied"
      | .[0].evidence = {"kind":"command","path":"app/cli/test/e2e/delivery-outcomes.sh"}
      | .[1].claim = "partial")' \
  "$NEEDS_REVIEW_REPORT" > "$NEEDS_REVIEW_REPORT.tmp"
mv "$NEEDS_REVIEW_REPORT.tmp" "$NEEDS_REVIEW_REPORT"
specgate delivery submit "$NEEDS_REVIEW_REF" --file "$NEEDS_REVIEW_REPORT" --json --no-input \
  | jq -e '.ok == true and .data.status.verdict == "needs_human_review"' > /dev/null
specgate delivery status "$NEEDS_REVIEW_REF" --detail --json \
  | jq -e '.ok == true and .data.verdict == "needs_human_review" and any(.data.per_criterion[]; .verdict == "unclear")' > /dev/null

echo "--- human-approved auto-archive outcome"
force_deterministic_review_settings "true"
AUTO_ARCHIVE_REF="$(create_work "E2E auto-archive smoke $RUN_ID" bound-check)"
echo "    work_ref=$AUTO_ARCHIVE_REF"
AUTO_ARCHIVE_REPORT="$SMOKE_WORKDIR/auto-archive-completion.json"
scaffold_completion "$AUTO_ARCHIVE_REF" "$AUTO_ARCHIVE_REPORT"
jq \
  --arg agent "e2e-builder-$USER_RUN_ID" \
  '.agent.name = $agent
    | .summary = "All evidence complete."
    | .affected_files = ["app/cli/test/e2e/delivery-outcomes.sh"]
    | .checks = [{"name":"tests","command":"test -f app/cli/test/e2e/delivery-outcomes.sh","status":"pass","detail":"deterministic smoke"}]
    | .criteria |= map(.claim = "satisfied"
      | .evidence = {"kind":"command","path":"app/cli/test/e2e/delivery-outcomes.sh"})' \
  "$AUTO_ARCHIVE_REPORT" > "$AUTO_ARCHIVE_REPORT.tmp"
mv "$AUTO_ARCHIVE_REPORT.tmp" "$AUTO_ARCHIVE_REPORT"
specgate delivery submit "$AUTO_ARCHIVE_REF" --file "$AUTO_ARCHIVE_REPORT" --run-checks --yes --json --no-input \
  | jq -e '.ok == true and .data.status.verdict == "pass"' > /dev/null

specgate work list --json \
  | jq -e --arg ref "$AUTO_ARCHIVE_REF" \
    '.ok == true and ([.data.needs_attention[]? | select(.change_request_key == $ref)] | length) == 1' > /dev/null
specgate delivery approve "$AUTO_ARCHIVE_REF" --yes --json --no-input \
  | jq -e '.ok == true and .data.verdict == "pass" and .data.executor == "human"' > /dev/null

specgate work show "$AUTO_ARCHIVE_REF" --json \
  | jq -e --arg ref "$AUTO_ARCHIVE_REF" '.ok == true and .data.change_request_key == $ref' > /dev/null
specgate work list --json \
  | jq -e --arg ref "$AUTO_ARCHIVE_REF" \
    '.ok == true and ([.data.needs_attention[]? | select(.change_request_key == $ref)] | length) == 0' > /dev/null

echo "OK: delivery outcome smoke passed for $NEEDS_REVIEW_REF and $AUTO_ARCHIVE_REF"
