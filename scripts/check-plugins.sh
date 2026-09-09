#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"
CANONICAL=plugins
DESTINATIONS="app/doc-registry/internal/agentpackages/plugins app/cli/internal/command/local_plugin_assets"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

python3 app/doc-registry/scripts/generate-plugin-metadata.py \
  --plugin-dir "$CANONICAL" --check

for embedded in $DESTINATIONS; do
  diff -r -x .DS_Store "$CANONICAL" "$embedded" >/dev/null ||
    fail "embedded plugins differ: $embedded"
done
for file in app/cli/internal/command/local_plugin_assets/.agents/plugins/marketplace.json \
  app/cli/internal/command/local_plugin_assets/.agents/plugins/personal-marketplace.json; do
  git ls-files --error-unmatch "$file" >/dev/null 2>&1 ||
    fail "embedded plugin asset is not tracked: $file"
done

skill_lines="$(find "$CANONICAL/skills" -name SKILL.md -exec cat {} + | wc -l | tr -d ' ')"
[ "$skill_lines" -le 650 ] ||
  fail "canonical skills have $skill_lines lines (limit: 650); simplify first"

if grep -rn 'resolve_work_item\|list_work_items\|report_implementation_feedback\|trigger_delivery_review' \
  "$CANONICAL/skills" "$CANONICAL/hooks" "$CANONICAL/rules" 2>/dev/null; then
  fail "plugins contain legacy tool names"
fi
if grep -rn '\.claude/skills/using-specgate\|legacy global skill\|older global skill' \
  "$CANONICAL/hooks" 2>/dev/null; then
  fail "plugin hooks reference stale global fallbacks"
fi

for skill in specgate specgate-project-setup specgate-work-preparation specgate-work-delivery; do
  path="$CANONICAL/skills/$skill/SKILL.md"
  [ -f "$path" ] && grep -Fxq "name: $skill" "$path" ||
    fail "missing skill or mismatched frontmatter: $skill"
done
for phrase in "specgate doctor --json" "specgate work show" "specgate work context" \
  "specgate delivery report" "specgate change submit" "specgate change status"; do
  grep -rFq "$phrase" "$CANONICAL/skills" "$CANONICAL/rules" ||
    fail "missing required CLI command: $phrase"
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$tmp/home/work/plain/.git" "$tmp/home/work/governed/.git" \
  "$tmp/home/work/governed/.specgate" "$tmp/cli" \
  "$tmp/project/.claude/specgate-hooks" "$tmp/project/.claude/skills"
cp -R "$CANONICAL/." "$tmp/cli"
printf '%s\n' specgate-plugin-v1 > "$tmp/cli/.specgate-owned"
cp "$CANONICAL/hooks/session-start" "$tmp/project/.claude/specgate-hooks/"
cp -R "$CANONICAL/skills/specgate" "$tmp/project/.claude/skills/"
printf '%s\n' specgate-plugin-v1 > "$tmp/project/.claude/specgate-hooks/.specgate-owned"

hook="$ROOT/$CANONICAL/hooks/session-start"
for config in hooks.json hooks-claude.json; do
  jq -e '.hooks.SessionStart | all(.[]; (.matcher // "") == "")' "$CANONICAL/hooks/$config" >/dev/null ||
    fail "$config filters out session lifecycle events"
done
"$hook" codex | jq -e '.hookSpecificOutput.hookEventName == "SessionStart" and (.hookSpecificOutput.additionalContext | type == "string") and (has("additionalContext") | not)' >/dev/null ||
  fail "Codex hook output does not match SessionStart schema"
native="$(cd "$tmp/home/work/plain"; "$hook" codex | jq -r .hookSpecificOutput.additionalContext)"
governed="$(cd "$tmp/home/work/governed"; "$hook" codex | jq -r .hookSpecificOutput.additionalContext)"
cli="$(cd "$tmp/home/work/plain"; "$tmp/cli/hooks/session-start" codex | jq -r .hookSpecificOutput.additionalContext)"
project="$(cd "$tmp/home/work/plain"; "$tmp/project/.claude/specgate-hooks/session-start" claude | jq -r .hookSpecificOutput.additionalContext)"

mkdir -p "$tmp/home/work/governed/subdir" "$tmp/home/work/governed/nested/.git"
for platform in codex claude; do
  wrapped="$(cd "$tmp/home/work/governed/subdir"; bash "$ROOT/$CANONICAL/hooks/run-hook.cmd" session-start "$platform")"
  printf '%s' "$wrapped" | jq -e '.hookSpecificOutput.additionalContext | contains("governed by SpecGate")' >/dev/null ||
    fail "$platform wrapper did not route from a repository subdirectory"
  isolated="$(cd "$tmp/home/work/governed/nested"; "$hook" "$platform")"
  printf '%s' "$isolated" | jq -e '.hookSpecificOutput.additionalContext | contains("governed by SpecGate") | not' >/dev/null ||
    fail "$platform hook inherited governance across a repository boundary"
done
grep -Fxq 'alwaysApply: true' "$CANONICAL/rules/using-specgate.mdc" ||
  fail "Cursor routing rule is not always applied"

printf '%s' "$native" | grep -Fq 'load `specgate`' ||
  fail "hook did not route explicit SpecGate work"
printf '%s' "$native" | grep -Fq 'IDE plugin manager owns' ||
  fail "hook did not identify native ownership"
printf '%s' "$native" | grep -Fq 'governed by SpecGate' &&
  fail "hook claimed governance outside a governed repository"
printf '%s' "$governed" | grep -Fq 'governed by SpecGate' ||
  fail "hook did not route governed repository work"
printf '%s' "$cli" | grep -Fq 'SpecGate CLI owns' ||
  fail "hook did not identify CLI ownership"
printf '%s' "$project" | grep -Fq 'SpecGate CLI owns' ||
  fail "project-local hook did not identify CLI ownership"
printf '%s' "$native" | grep -Fq '# Using SpecGate' &&
  fail "hook injected the full router"

echo "plugin checks passed ($skill_lines canonical skill lines)"
