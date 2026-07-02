---
name: delivering-work
description: Delivery. Use when starting or resuming a SpecGate work item, change request, tracker reference, or Context Pack, implementing inside approved scope, verifying, reporting completion evidence, or reworking a failed delivery verdict.
---

# Delivering Work

## Invocation

Invocation mode: [lifecycle phase](../using-specgate/SKILL.md#invocation).
Direct triggers: starting or resuming implementation from a work reference, or
verifying, reporting, and reviewing a delivery.

## 1. Pick up and confirm readiness

Resolve the work reference and inspect governance state:

```bash
specgate status --json
specgate work show "$WORK_REF" --json
specgate gates status "$WORK_REF" --json
specgate work policy "$WORK_REF" --json
```

Stop before implementation when the work is not ready: required human approval
absent, required gates not acceptable, acceptance criteria missing or
placeholders, or the Context Pack stale. Tell the human the concrete missing
condition. A readiness pass is not human approval.

Completion criterion: either the ready state is confirmed, or the human has a
specific blocker to resolve.

## 2. Read the Context Pack

```bash
specgate work context "$WORK_REF" --json
```

The approved artifact and Context Pack outrank chat history, tracker comments,
and stale repo docs. Read acceptance criteria, scope limits, non-goals, design
references, risks, and verification requirements before touching files.

Fetch artifact files when the pack needs more detail, using the paths the pack
or artifact metadata lists (filenames below are examples):

```bash
specgate artifact files "$ARTIFACT_ID" spec.md acceptance.md --json
```

For quick-route work, an approved stored Context Pack is the implementation
contract even when no lead or canonical artifact exists. If the Context Pack is
missing or not generated, report it as a handoff blocker instead of working
around it.

Completion criterion: the implementation contract, non-goals, and required
verification are understood before editing.

## 3. Implement inside scope

Implement only the Context Pack scope and acceptance criteria. Preserve explicit
non-goals and blast-radius limits. Ground UI changes in attached design
references. Write or update tests at the narrowest useful level and keep
repository-owned docs synchronized with shipped behavior.

If approved artifact content itself must change, create a draft proposal —
never mutate approved state or silently change approved scope:

```bash
specgate artifact propose "$ARTIFACT_ID" --file proposal.json --json
```

When scope is ambiguous or contradictory, do not guess it into the
implementation. Record the signal with a JSON body containing the
`change_request_id` from `specgate work show` or the Context Pack:

```json
{"change_request_id":"$CHANGE_REQUEST_ID","event_type":"coding_agent.blocked_ambiguity","severity":"blocking","summary":"Implementation contract needs clarification."}
```

```bash
specgate delivery report "$WORK_REF" --file blocked.json --json
```

In an interactive session, ask the human for the missing decision and capture
the resolution in a draft artifact proposal. In a headless session, poll
`specgate delivery status "$WORK_REF" --detail --json`. The same report shape
with `event_type: "coding_agent.docs_updated"`, severity `info`, records
repository-doc updates.

Completion criterion: every change maps to an acceptance criterion, required
verification item, or required repository-doc update; ambiguity is resolved or
recorded, never guessed.

## 4. Verify

Run verification proportional to the change: tests, types, lint, build. Record
each command, its result, and the reason for any intentionally skipped check.

Completion criterion: every required or skipped check has a command, result,
and reason.

## 5. Report completion and submit for review

Scaffold the completion report — it prefills `event_type` and one `criteria[]`
entry per acceptance criterion with the correct `criterion_id` and text:

```bash
specgate delivery report "$WORK_REF" --init --json
```

Fill in `summary`, `affected_files`, `checks` (with skipped-check reasons), and
one claim per criterion: `satisfied`, `partial`, or `not_done`. Do not claim
`satisfied` when the check was not run or evidence is unclear. Criterion
`evidence` is one object, not an array, in the delivery evidence shape:
`{kind, path?, line?, file_key?, heading?, url?}`. No free-form fields such as
`detail` — put short command or file references in `path` or `url`.

Submit the delivery tail in one command — it reports completion, runs gates,
triggers delivery review, and returns the per-criterion verdict:

```bash
specgate delivery submit "$WORK_REF" --file completion.json --json
```

If a stage fails, the error envelope names it in `error.details.stage`. The
individual commands remain available for one stage at a time; none require
`--yes` in non-interactive runs:

```bash
specgate delivery report "$WORK_REF" --file completion.json --json
specgate gates run "$WORK_REF" --json
specgate delivery review "$WORK_REF" --json
specgate delivery status "$WORK_REF" --detail --json
```

Pending artifact-governance gates may be non-applicable for quick-route work
with an approved stored Context Pack — do not misreport them as passed.

Completion criterion: every acceptance criterion has exactly one claim with
concrete evidence, and the submit result is captured.

## 6. Rework until the verdict is clear

If the verdict fails, use `outstanding_md` and per-criterion findings to drive
the next focused fix: fix only the identified gap, re-run verification, update
`completion.json` reusing the same `criterion_id` values (such as `ac-0`) so the
reviewer can match claims to criteria, and run `delivery submit` again.

A `needs_human_review` verdict is not a fail — the report was too thin to
judge. The reviewer runs server-side with no repository checkout, so it judges
your reported `criteria[].claim`, `checks[]`, and `summary`, not the files. A
bare `{kind, path}` pointer with no substantive summary or checks reads as
unverifiable. Resubmit with a real `checks[]` array and a summary stating the
concrete change — not a new evidence field.

Completion criterion: the persisted verdict is `pass`, or the remaining blocker
is explicit and externally owned.
