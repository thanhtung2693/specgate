---
name: preparing-work
description: Preparation. Use when turning a fuzzy request, PRD, spec, or planning discussion into SpecGate-ready vertical-slice work, publishing or updating an artifact package, or checking whether an artifact is ready for human review or handoff.
---

# Preparing Work

## Invocation

Invocation mode: [lifecycle phase](../using-specgate/SKILL.md#invocation).
Direct triggers: shaping fuzzy intent into work items, artifact publishing,
artifact readiness checks, readiness repair, or handoff-readiness checks.

## 1. Gather the source material

Read the user's request, available PRD/spec/task notes, relevant repo docs, and
nearby code only far enough to understand the existing domain language and
likely integration boundaries.

Completion criterion: the problem, intended outcome, known constraints, and
open questions are captured without inventing missing product intent.

## 2. Draft vertical slices

Create tracer-bullet work items: each slice delivers a narrow, verifiable path
through the necessary layers rather than a horizontal layer-only task. Keep
prefactoring separate only when it must happen before any slice can land.

For each slice, include: title; what to build; acceptance criteria; blockers or
dependency order; out-of-scope notes.

Completion criterion: every slice is independently reviewable or demoable with
testable acceptance criteria and clear non-goals, and no slice contains
unrelated cleanup. If the source material is ambiguous, ask the human for the
missing decision instead of guessing.

## 3. Publish the artifact package

If the task names an existing work item, inspect it first:

```bash
specgate work show "$WORK_REF" --json
```

Otherwise publish the prepared artifact package:

```bash
specgate artifact publish --file artifact.json --json
```

The `--file` argument is any JSON file you write for the publish body — not the
target repository's dependency manifest such as `package.json`. This
feature-key guidance applies to feature-backed spec artifact publishing, not
quick-route CR creation; do not invent a Feature for a small bugfix or quick
Context Pack just to publish a spec package.

Before feature-backed publishing, point `artifact.json.feature_key` at the
right feature — a key that matches no existing feature silently creates a
duplicate feature:

- Continues an existing feature → reuse that feature's exact key.
  `specgate work show "$WORK_REF" --json` already reports `feature_key` when
  working from a work item; otherwise look it up with
  `specgate feature list --search <topic>`. The SpecGate UI Features view is a
  human fallback, not the agent's canonical lookup path.
- Genuinely new work → use a new key.
- Unclear which existing feature it belongs to → surface the candidate keys to
  the human and let them choose. Do not guess.

Completion criterion: the artifact id and published version are known, or the
publish error is reported without editing implementation code.

## 4. Run artifact readiness checks

```bash
specgate gates check "$ARTIFACT_ID" --json
```

Completion criterion: the aggregate result, every gate verdict, and all
covered, partial, and missing contract topics are captured.

## 5. Repair the gaps

Fix only the source documents that own each gap. Do not invent missing product
intent — when a gap is ambiguous, call it out as a human decision instead of
filling it with plausible prose.

Completion criterion: every gap is either fixed in the right document or listed
as requiring human judgment.

## 6. Republish and recheck

Set the artifact's `base_version` inside `artifact.json` so the server can
reject a stale revision. Publish the corrected version and run the readiness
checks again:

```bash
specgate artifact publish --file artifact.json --json
specgate gates check "$ARTIFACT_ID" --json
```

If readiness is unavailable, report the service error and preserve the
published artifact. Do not bypass governance or claim the checks passed.

Completion criterion: the aggregate is clean, or remaining warnings are
explicitly named as human-review items.

## 7. Stop before coding

A clean readiness result means ready for human review, not approved. This skill
prepares work; switch to `delivering-work` for approved implementation handoff.

Completion criterion: no implementation files are edited under this skill.
