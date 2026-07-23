---
name: specgate-work-delivery
description: Use when implementing, resuming, verifying, or reworking an approved SpecGate work item or Context Pack, or when SpecGate status names the implementing agent.
---

# Delivering Work

Apply the [SpecGate operating contract](../specgate/SKILL.md#operating-contract).
This phase implements the approved contract, then stops at the next actor.
Never approve an artifact or make a human delivery decision.

## 1. Load the exact contract

Start with the named work item:

```bash
specgate work show "$WORK_REF" --json
specgate work context "$WORK_REF" --json
specgate change status "$WORK_REF" --json
```

Stop before editing when approval is absent, the Context Pack is stale, or
criteria are missing or placeholders. Quick work may lack an artifact. If
artifact-backed work lacks its canonical approved version, hand the blocker to
the human or `specgate-work-preparation`.

Read only `change status.data`:

- If `data.next_actor` is not `implementing_agent`, hand off without editing.
- For `review_pending`, require `next_command` to be exactly one `specgate` CLI
  command without shell operators. Otherwise hand off. Run it verbatim
  immediately before sections 2–5. On failure, go to section 7. On success,
  reread only Change status and route again; stop if still `review_pending`.
- For `rework_requested`, record `guidance` and `missing`, then carry this
  sequence through later steps: focused fix, affected checks, updated evidence,
  submit, fresh status. Never submit old evidence first.
- Otherwise record `missing` and `guidance`, then enter `next_command`'s named
  section; stop if none matches.

Completion criterion: every change/check maps to a criterion or required
repository-doc update.

## 2. Resume safely

When resuming, check the persisted receipt:

```bash
specgate change status "$WORK_REF" --json
```

Continue only when `freshness` confirms a match. Stop on any mismatch signaled
by `stale: true`. Treat an unavailable comparison as a blocker. Receipt metadata
is not source, a patch, or cryptographic proof.

Completion criterion: the receipt matches this checkout, or mismatch is a
human-owned blocker.

## 3. Check artifact drift

Skip when no artifact exists. Otherwise dispatch tasks, locate
`spec_repo_drift`, and follow its frozen `skill_content`:

```bash
specgate gates tasks dispatch "$ARTIFACT_ID" --json
specgate gates tasks list "$ARTIFACT_ID" --json
specgate gates tasks show <task-id> --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
```

Use exact task digests. Put `examined_docs` and `repo_commit` under `evidence`;
keep `findings` top-level. Report out-of-scope drift without editing it.

Completion criterion: the exact drift task has a result, or no artifact exists.

## 4. Implement and verify

Implement approved scope and preserve non-goals. Record required tests, lint,
type checks, builds, and every skip reason. Never mutate an approved snapshot;
a new artifact version belongs to `specgate-work-preparation`.

For a pull or merge request, include
`<!-- specgate-work-ref: $WORK_REF -->` in its description. Do not infer work
identity from branches, titles, commits, filenames, headings, or keywords.

Completion criterion: every criterion is implemented or explicitly not done,
and every required check has an observed result or explained skip.

## 5. Report criterion evidence

Create the CLI-owned scaffold:

```bash
specgate delivery report "$WORK_REF" --init --json
```

Keep returned `data.path` verbatim as `$COMPLETION_PATH`. When command reports
an existing regular scaffold, reuse exact `error.details.path` only when
`change_request_id` matches `work show`; when `context_digest` is present, it
must match the Context Pack. Never overwrite automatically. Stop when an
existing file cannot be attributed safely.

Fill `agent.name`, `summary`, `affected_files`, `checks[]`, and exactly one `criteria[]`
entry per canonical criterion. Give each criterion an independently reviewable
claim and evidence path, line, heading, file key, or URL; a command name alone
is not evidence. Replace every `pending` check with `pass`, `fail`, or `skipped` plus
observed detail; submission rejects untouched placeholders. Evidence paths must
exist. `satisfied` cannot depend on a failed, missing, or skipped required check.
Every non-skipped `checks[].command` must be non-interactive and valid for
`sh -c`.

Review the completion file, especially its shell commands, then submit:

```bash
specgate change submit "$WORK_REF" \
  --file "$COMPLETION_PATH" --run-checks --yes --json
specgate change status "$WORK_REF" --json
```

`--run-checks` replaces self-reported results with observed results. Fix failures
before claiming completion.

Completion criterion: submission succeeded and fresh Change status was read.

## 6. Follow the authoritative actor

Use only `change status.data`. For `implementing_agent`, complete `missing`, run
the supplied SpecGate `next_command` at its named step, then read status again.
A scaffold command does not complete work.

Hand off `next_command` verbatim.

For `human_reviewer`, `maintainer`, or `none`, stop. `awaiting_review` belongs to
the human reviewer. Run peer review only when the human explicitly requests it;
use a different review-only agent:

```bash
specgate delivery peer-review "$WORK_REF" --init --json
specgate delivery peer-review "$WORK_REF" \
  --file "$PEER_REVIEW_PATH" --json
```

Keep its returned `data.path` verbatim as `$PEER_REVIEW_PATH`; apply the same
existing-file rule.

A pass means ready for human review, not accepted. Implementing agent never runs
accept, request-changes, or another human-decision command.

Completion criterion: the implementing agent has no remaining authoritative
action, or one exact blocker is reported.

## 7. Show the delivery handoff

Read `specgate change status "$WORK_REF" --json` immediately before responding.
For `awaiting_acceptance`, render:

```text
SpecGate delivery receipt — <title> (<ref>)
Evidence: <evidence>
Assurance: <assurance>
Decision: <decision>
Receipt: <receipt>
Freshness: <freshness>
[Stale: <stale_reason> — only when stale]
Next (<next_actor>): <next_command>
```

Preserve values verbatim. Stale is a warning, not a state override. For any
other state, show a compact `SpecGate delivery handoff` with state, missing,
`next_actor`, and `next_command`; do not use success wording. If status fails,
report failure. If state is `accepted`, echo it without claiming this agent
accepted it.

In Full mode only, use the URL returned by
`specgate open "$WORK_REF" --print --json`. In Local mode, never call `open`.
Do not read the completion file again or call stats or audit. Never claim cleanup
eligibility, bugs prevented, time saved, accepted, or delivered without
authoritative status.

Completion criterion: the response names state, evidence quality, freshness,
next actor, and next command without crossing authority.
