---
name: specgate-work-delivery
description: Delivery. Use when starting or resuming a SpecGate work item, change request, tracker reference, or Context Pack, implementing inside approved scope, verifying, reporting completion evidence, or reworking a failed delivery verdict.
---

# Delivering Work

## Invocation

Invocation mode: [lifecycle phase](../specgate-router/SKILL.md#invocation).
Use for implementation, verification, completion evidence, and delivery rework.
A work reference (`CR-123`, tracker mapping, or Context Pack URI) makes ordinary
developer phrasing such as "fix this" governed work. Do not stop after local code
and tests: read the pack, report evidence, and submit delivery review.

## 1. Confirm readiness

In Full mode:

```bash
specgate work list --phase ready --json
specgate status --json
specgate work show "$WORK_REF" --json
specgate gates status "$WORK_REF" --json
specgate work policy "$WORK_REF" --json
```

In Local mode, use `work show` and `work context` only. Local has no
server-side `gates status` or `work policy`; artifact readiness was checked
before the human approved and promoted the artifact.

Stop when approval is absent, required gates are unacceptable, criteria are
missing or placeholders, or the Context Pack is stale. Name the exact blocker.
A readiness pass is not human approval.

Completion criterion: work is ready or a human-owned blocker is explicit.

## 2. Read the contract

```bash
specgate work context "$WORK_REF" --json
```

The approved artifact and Context Pack outrank chat, tracker comments, and stale
repo docs. Read criteria, scope, non-goals, risks, design references, and required
verification before editing. In Full mode, fetch an artifact file only when the
Context Pack is insufficient:

```bash
specgate artifact files "$ARTIFACT_ID" --json
specgate artifact files "$ARTIFACT_ID" "<exact-path-from-list>" --content --json
```

Quick-route packs are authoritative without a canonical artifact. For
feature-backed work, `No canonical spec content found` after approval means the
artifact was not promoted. Retry the exact original combined approval command,
including its title, description, and every acceptance criterion, then
regenerate:

```bash
specgate --yes change approve "$ARTIFACT_ID" \
  --title "$WORK_TITLE" \
  --ac "$CONFIRMED_CRITERION_1" \
  --ac "$CONFIRMED_CRITERION_2"
specgate work context "$WORK_REF" --json
```

Never promote an unapproved artifact or work around a missing pack.

Completion criterion: contract and non-goals are understood before editing.

### Resume from a persisted Git receipt

When a new IDE agent resumes a work item, inspect the latest receipt before
touching the checkout:

```bash
specgate delivery status "$WORK_REF" --detail --json
```

Compare `git_receipt.repository` with `git remote get-url origin`, not with the
checkout directory. A local origin may legitimately be a sibling clone.
Compare `branch`, `head_revision`, and `changed_files` with
`git branch --show-current`, `git rev-parse HEAD`, and `git status`; the
`diff_digest` identifies that persisted snapshot and is not a path. Stop and
ask the human when repository identity, revision, or checkout changes mismatch.
Do not attribute listed plugin/config files to the origin checkout: they are
current-checkout state and should be treated as unrelated changes unless this
work owns them. The receipt contains metadata and a digest only, never source
code or a patch. It is **Agent-reported** evidence that detects stale/drift
between report and submit and gives a resume comparison target—not
cryptographic proof. A marked, merged PR/MR corroborates repository activity
only when its head SHA matches the latest submitted `git_receipt.head_revision`.

## 3. Check spec-repo drift

When `$ARTIFACT_ID` exists, dispatch the drift gate in either Local CLI or Full mode:

```bash
specgate gates tasks dispatch "$ARTIFACT_ID" --json
specgate gates tasks list "$ARTIFACT_ID" --json
specgate gates tasks show <task-id> --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
```

Read the `spec_repo_drift` task's `skill_content`, inspect the named docs, and
submit this shape using the exact task values:

```json
{
  "gate": "spec_repo_drift",
  "gate_digest": "<task gate_digest>",
  "input_digest": "<task artifact_digest>",
  "state": "pass",
  "summary": "No semantic contradictions found.",
  "evaluator": {"executor": "ide_agent", "name": "<agent name>"},
  "evidence": {
    "examined_docs": ["AGENTS.md", "docs/relevant-contract.md"],
    "repo_commit": "<git rev-parse HEAD>"
  },
  "findings": []
}
```

Both `evidence.examined_docs` and `evidence.repo_commit` are required and must
be nested under `evidence`; top-level fields are invalid. The approved spec wins
over drifted docs. Drift warns and never approves or blocks delivery. Report
out-of-scope drift through the gate; fix it only when this work already owns
that documentation. Skip only when no artifact exists.

Completion criterion: drift is checked or explicitly inapplicable.

## 4. Implement inside scope

Map every change to a criterion, verification item, or required repo-doc update.
Preserve non-goals and blast-radius limits. Test at the narrowest useful level;
run the full module command for shared types, interfaces, migrations, or imported
packages.

If approved artifact content must change, publish a new artifact version rather
than mutating it:

```bash
specgate artifact publish --file artifact.json
```

Use the normal artifact publish package with the updated files and feature
identity. For ambiguity, report
`coding_agent.blocked_ambiguity` with blocking severity and ask the human. Use
`coding_agent.docs_updated` for repository-doc evidence.

When creating or updating a pull/merge request, put the exact machine marker
`<!-- specgate-work-ref: $WORK_REF -->` in its description. SpecGate does not
infer work identity from branch names, titles, commits, or surrounding prose.

Completion criterion: all changes are in scope; ambiguity is resolved or reported.

## 5. Verify and report

Record every test, type, lint, and build command with its observed result and the
reason for any skip. Delivery review trusts reported checks; it does not run the
suite itself.

Scaffold one claim per acceptance criterion:

```bash
specgate delivery report "$WORK_REF" --init --json
```

Fill `agent.name` with this coding agent's stable name, then fill `summary`,
`affected_files`, `checks[]`, and `criteria[]`. Keep
`criteria[].claim` exactly one of those enum values: `satisfied`, `partial`, or
`not_done`; never use `satisfied` when its check did not run.

- `checks[]`: `{name, command?, status, detail?}` with status
  `pass|fail|skipped`. Put the observed result (for example, a pass count) in
  `detail`, not just the command name.
- `criteria[].evidence`: one `{kind, path?, line?, file_key?, heading?, url?}`.
  Make each criterion claim independently reviewable: summarize the behavior and
  point `evidence.path` and `line` at proof. When one test covers multiple
  criteria, identify the assertion proving each one.
- A criterion bound with `@check:<name>` needs the matching `checks[].name`.
  Passed maps to `met`, failed to `unmet`, and missing/skipped to `unclear`.
- Evidence paths must exist in the working tree. `--skip-evidence-check` is an
  explicit bypass, not the normal path.

Completion criterion: every criterion has one evidence-backed claim.

## 6. Submit and rework

```bash
specgate change submit "$WORK_REF" \
  --file ".specgate/completion-$WORK_REF.json" --run-checks --yes --json
specgate change status "$WORK_REF" --json
```

`--run-checks` reruns non-skipped commands and replaces claims with observations.
It executes shell commands from the completion file, so review that file before
passing `--yes`. Fix observed failures. Delivery review remains available for
diagnosis in both modes:

```bash
specgate delivery review "$WORK_REF" --json
```

In Full mode, rerun the work-item model gates separately when needed:

```bash
specgate gates run "$WORK_REF" --json
```

If Change status is `awaiting_review` and its assurance is **Agent-reported**,
ask a different review-only agent to inspect the Context Pack, checkout, checks,
and latest completion receipt. It records cooperative evidence, never approval:

```bash
specgate delivery peer-review "$WORK_REF" --init --json
specgate delivery peer-review "$WORK_REF" --file ".specgate/peer-review-$WORK_REF.json" --json
```

Fill the scaffold's reviewer `agent.name` and narrative `summary`, then fill
every canonical criterion exactly once. Each criterion uses
`claim: satisfied|partial|not_done` plus
`evidence: {kind, path?, line?, file_key?, heading?, url?}`. Put review prose in
the top-level summary; `evidence.summary` is not the portable evidence shape.
Do not reuse the scaffold after a new completion. Full-mode handoffs may use
the URL from `specgate open --print --json`; Local handoffs name the work ID and
exact next CLI command.

For a failed verdict, use `outstanding_md` and per-criterion findings for one
focused fix, rerun verification, reuse criterion ids, and resubmit. Use
`specgate audit "$WORK_REF" --json` when the trail is unclear.

`needs_human_review` means the report is too thin to judge, not that delivery
passed. `reason_code: policy_unavailable` is a maintainer-owned fail-closed
block. `reason_code: delivery_review_outdated` means a newer completion needs
its own `specgate delivery review "$WORK_REF" --json` run before any human
decision.

A persisted platform `pass` means ready for human review, not accepted or
delivered. Check the actionable projection:

```bash
specgate change status "$WORK_REF" --json
```

The completion reporter must not run the human accept command. If a human
requests changes, that decision remains bound to the exact completion; make one
focused correction, submit a new completion, and start a new review cycle.
Finish agent work when status is `awaiting_acceptance`, or when the remaining
named blocker is owned by a human or maintainer.

Completion criterion: the verdict is captured and any rework is evidence-driven.
