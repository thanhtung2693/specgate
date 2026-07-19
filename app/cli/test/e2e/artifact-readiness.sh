#!/usr/bin/env bash
# e2e artifact readiness smoke test — requires a running SpecGate stack.
# Usage:
#   SPECGATE_SERVER=http://localhost:8080 bash app/cli/test/e2e/artifact-readiness.sh
#
# The script uses a temporary HOME, logs in as a disposable local user, publishes
# a full role-tagged artifact package, verifies stored files, runs artifact
# readiness, and exercises IDE-agent gate task preview/dispatch.
set -euo pipefail

SPECGATE_SERVER="${SPECGATE_SERVER:-http://localhost:8080}"
export SPECGATE_SERVER

RUN_ID="${SPECGATE_E2E_RUN_ID:-$(date +%s)}"
USER_RUN_ID="${RUN_ID//[^[:alnum:]]/-}"
USER_RUN_ID="${USER_RUN_ID:0:24}"
REMOVE_SMOKE_HOME=0
REMOVE_SMOKE_WORKDIR=0
if [ -z "${SMOKE_HOME:-}" ]; then
  SMOKE_HOME="$(mktemp -d /tmp/specgate-artifact-e2e-home.XXXXXX)"
  REMOVE_SMOKE_HOME=1
fi
if [ -z "${SMOKE_WORKDIR:-}" ]; then
  SMOKE_WORKDIR="$(mktemp -d /tmp/specgate-artifact-e2e-work.XXXXXX)"
  REMOVE_SMOKE_WORKDIR=1
fi
export HOME="$SMOKE_HOME"

cleanup() {
  if [ "$REMOVE_SMOKE_HOME" -eq 1 ]; then
    rm -rf "$SMOKE_HOME"
  fi
  if [ "$REMOVE_SMOKE_WORKDIR" -eq 1 ]; then
    rm -rf "$SMOKE_WORKDIR"
  fi
}
trap cleanup EXIT

SPEC_FILE="$SMOKE_WORKDIR/spec.md"
PLAN_FILE="$SMOKE_WORKDIR/plan.md"
VERIFY_FILE="$SMOKE_WORKDIR/verification.md"
PACKAGE_FILE="$SMOKE_WORKDIR/artifact.json"

cat > "$SPEC_FILE" <<'SPEC'
# Full artifact E2E spec

## Goal
Allow a new workspace user to publish a governed feature artifact and verify it before handoff.

## Scope
- Publish an artifact with role-tagged documents.
- Review the stored document list and content.
- Run artifact-scoped readiness checks.
- Preview and dispatch IDE-agent gate tasks.

## Non-goals
- No production deployment.
- No tracker issue creation.

## Acceptance Criteria
1. The artifact publishes as a draft with a server-assigned version.
2. The artifact exposes the original spec document through the files API.
3. Readiness checks return an aggregate result and gate evidence.
4. Gate-task preview and dispatch identify the expected artifact gates.

## Constraints
Use only the local SpecGate stack and disposable test data.

## Risks
LLM providers may be unavailable; deterministic checks should still return a useful response.

## Verification
Run the SpecGate CLI commands in this smoke workflow and inspect JSON envelopes.
SPEC

cat > "$PLAN_FILE" <<'PLAN'
# Implementation Plan

1. Publish the artifact package.
2. Confirm artifact metadata and files.
3. Run gates check.
4. Dispatch gate tasks for IDE-agent review.
PLAN

cat > "$VERIFY_FILE" <<'VERIFY'
# Verification Plan

- `specgate artifact show <artifact>` returns the artifact id and status.
- `specgate artifact files <artifact> spec.md --content` returns this spec.
- `specgate gates check <artifact>` returns an aggregate readiness value.
- `specgate gates tasks dispatch <artifact>` returns artifact-scoped gate metadata.
VERIFY

cat > "$PACKAGE_FILE" <<JSON
{
  "feature_key": "e2e-full-artifact-$RUN_ID",
  "feature_name": "E2E Full Artifact Smoke",
  "source_kind": "cli_e2e",
  "source_id": "full-artifact-smoke-$RUN_ID",
  "source_revision": "local",
  "authority": "proposed",
  "request_type": "new_feature",
  "impact_level": "medium",
  "requested_governance_level": "standard",
  "impact_declaration": {
    "protected_domains": [],
    "protected_domains_status": "no",
    "affected_systems": ["specgate-cli"],
    "data_or_schema_change": "no",
    "external_contract_change": "no",
    "irreversible_or_complex_rollback": "no",
    "broad_blast_radius": "no",
    "expected_paths": ["app/cli/**"]
  },
  "documents": [
    {"path": "spec.md", "role": "spec", "source_file": "spec.md"},
    {"path": "plan.md", "role": "plan", "source_file": "plan.md"},
    {"path": "verification.md", "role": "verification", "source_file": "verification.md"}
  ]
}
JSON

echo "--- specgate doctor"
specgate doctor --no-input

echo "--- login disposable user/workspace"
specgate user login \
  --workspace "E2E Artifact Workspace $RUN_ID" \
  --display-name "E2E Artifact User" \
  --username "e2e-artifact-$USER_RUN_ID" \
  --email "e2e-artifact-$USER_RUN_ID@example.com" \
  --json --no-input | jq -e '.ok == true' > /dev/null

echo "--- publish artifact package"
PUBLISH_JSON="$(specgate artifact publish --file "$PACKAGE_FILE" --json --no-input)"
ARTIFACT_ID="$(printf '%s' "$PUBLISH_JSON" | jq -r '.data.artifact_id // empty')"
if [ -z "$ARTIFACT_ID" ]; then
  printf '%s\n' "$PUBLISH_JSON" >&2
  echo "artifact publish did not return artifact_id" >&2
  exit 1
fi
echo "    artifact_id=$ARTIFACT_ID"

echo "--- artifact show/files"
specgate artifact show "$ARTIFACT_ID" --json \
  | jq -e --arg id "$ARTIFACT_ID" '.ok == true and .data.id == $id and .data.status == "draft" and (.data.version | length > 0)' > /dev/null
specgate artifact files "$ARTIFACT_ID" --json \
  | jq -e '.ok == true and ((.data.items // .data.files // []) | length >= 3)' > /dev/null
specgate artifact files "$ARTIFACT_ID" spec.md --content --json \
  | jq -e '.ok == true and ((.data.files[0].content // "") | contains("Full artifact E2E spec"))' > /dev/null

echo "--- gates check"
specgate gates check "$ARTIFACT_ID" --json \
  | jq -e '.ok == true and (.data.aggregate | length > 0)' > /dev/null

echo "--- gate task dispatch"
specgate gates tasks dispatch "$ARTIFACT_ID" --json \
  | jq -e --arg id "$ARTIFACT_ID" '.ok == true and .data.artifact_id == $id and ((.data.created_task_ids | length) + (.data.skipped_gate_keys | length)) >= 1' > /dev/null

echo "OK: artifact readiness smoke passed for $ARTIFACT_ID"
