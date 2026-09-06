---
name: specgate-work-delivery
description: Use when implementing, resuming, verifying, or reworking an approved SpecGate work item or Context Pack, or when SpecGate status names the implementing agent.
---

# Delivering Work

Apply the [SpecGate operating contract](../specgate/SKILL.md#operating-contract).
Implement the approved contract, stop at the next actor, and never approve an
artifact or make a human delivery decision.

## 1. Load the exact contract

Use the mode from `doctor`. Local: read scope, criteria, verification contract,
document index, and status together:

```bash
specgate work resume "$WORK_REF" --json
```

Use `data.status`. Read pinned documents, including scope and non-goals:

```bash
specgate work context "$WORK_REF" --document "$DOCUMENT_PATH" --role "$ROLE" --json
```

Copy path/role from `data.documents`. Reuse only matching-digest content; the
index cannot replace it. Quick work uses persisted scope and criteria.

Full mode: read these; status is `change status.data`:

```bash
specgate work context "$WORK_REF" --json
specgate change status "$WORK_REF" --json
```

Stop before editing when approval is absent, the Context Pack is stale, or
criteria are missing/placeholders. Hand artifact-backed work missing its
approved version to the human or
`specgate-work-preparation`.

Reuse results until state changes; avoid duplicate Local reads. Follow status:

- If `data.next_actor` is not `implementing_agent`, hand off without editing.
- For `review_pending`, accept only one `specgate` `next_command` without shell
  operators; else hand off. Run it verbatim before sections 2–5. On failure, go
  to section 7. On success, reread Change status; stop if still `review_pending`.
- For `rework_requested`, carry `guidance` and `missing` through: focused fix,
  affected checks, updated evidence, submit, fresh status. Never submit old
  evidence first.
- Otherwise record `missing` and `guidance`, then follow `next_command`'s named
  section or stop if none matches.

Every change/check must map to a criterion or required repository-doc update.

Completion criterion: submitted evidence contains an observed result for every
required check, or its explicit skip reason.

## 2. Resume safely

For `next_actor=implementing_agent` in `implementation` or `rework_requested`,
inspect current edits, rerun checks, and submit fresh evidence. Otherwise
require matching `freshness`; stop if unavailable.

## 3. Check artifact drift

Skip when no artifact exists. Otherwise dispatch tasks, locate
`spec_repo_drift`, and follow its frozen `skill_content`:

```bash
specgate gates tasks dispatch "$ARTIFACT_ID" --json
specgate gates tasks list "$ARTIFACT_ID" --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
```

`tasks list` returns `skill_content` and both digests; never call `tasks show`.
Copy digests exactly. Put `examined_docs` and `repo_commit` under `evidence`;
keep `findings` top-level. Report out-of-scope drift without editing it.

## 4. Implement and verify

Implement approved scope and preserve non-goals. Record required tests, lint,
type checks, builds, and every skip reason. Never mutate an approved snapshot;
a new artifact version belongs to `specgate-work-preparation`.

For a pull or merge request, include
`<!-- specgate-work-ref: $WORK_REF -->` in its description. Never infer work
identity from branches, titles, commits, filenames, headings, or keywords.

## 5. Report criterion evidence

Create the CLI-owned scaffold:

```bash
specgate delivery report "$WORK_REF" --init --json
```

Keep returned `data.path` verbatim as `$COMPLETION_PATH`. Reuse an existing
regular scaffold's exact `error.details.path` only when its `change_request_id`
matches the Context Pack or status work ID and any `context_digest` matches the
Context Pack. Never overwrite automatically; stop when attribution is unsafe.

Fill `agent.name`, `summary`, `affected_files`, `checks[]`, and exactly one
`criteria[]` entry per canonical criterion. Anchor evidence with `line` or a
verbatim `heading`; a path is
`unanchored`, and a missing heading is `heading_not_found`. Neither has an
excerpt. A command name alone is not evidence. Set every check to `pass`,
`fail`, or `skipped`, or leave it `pending` with a runnable command and use
`--run-checks` for observation. Evidence paths must exist. `satisfied` cannot
depend on a failed, missing, or skipped required check.
Every non-skipped `checks[].command` must be non-interactive and valid for
`sh -c`.

Review its shell commands, then submit:

```bash
specgate change submit "$WORK_REF" \
  --file "$COMPLETION_PATH" --run-checks --yes --json
```

`change submit` returns the same status payload; use its `data`, never refetch.
`--run-checks` replaces self-reported results with observed results. Fix failures.

## 6. Follow the authoritative actor

Use the latest authoritative status. For `implementing_agent`, complete `missing`, run
the supplied `next_command` at its named step, then reread status. A scaffold
command does not complete work.

Hand off `next_command` verbatim.

For `human_reviewer`, `maintainer`, or `none`, stop. `awaiting_review` belongs to
the human reviewer. SpecGate requires no subagent solely for this lifecycle.
Run peer review only when the human explicitly requests it; use a
different review-only agent:

```bash
specgate delivery peer-review "$WORK_REF" --init --json
specgate delivery peer-review "$WORK_REF" \
  --file "$PEER_REVIEW_PATH" --json
```

Keep `data.path` as `$PEER_REVIEW_PATH`; same existing-file rule.

A pass means ready for human review, not accepted. The implementing agent never
runs a human-decision command.

Export a teammate-readable review request only when the human requests one:

```bash
specgate delivery handoff export "$WORK_REF" --json
```

Report `data.path` to commit, and `data.git_ignored` when true — an ignored
bundle never reaches the reviewer. Never commit or decide.

## 7. Show the delivery handoff

Use the latest status payload. If an action returns none, run
`specgate change status "$WORK_REF" --json`.
For `awaiting_acceptance`, render:

```text
SpecGate delivery receipt — <title> (<ref>)
Evidence: <evidence>
Assurance: <assurance>
Decision: <decision>
Receipt: <receipt>
Freshness: <freshness>
Acceptance criteria: <total> total · <met> met · <unmet> unmet · <unclear> unclear
[<verdict>] <criterion text> — <why>   (one line per data.criteria entry)
[Stale: <stale_reason> — only when stale]
Next (<next_actor>): <next_command>
```

Count `data.criteria` verdicts. If empty, list canonical Context Pack criteria
as `Acceptance criteria: <total> total · <total> not reviewed` followed by
`[not reviewed] <criterion text>`. `unclear` and unreviewed are never `unmet`.

Preserve values verbatim, including every criterion line. Stale is a warning,
not a state override. For any other state, show a `SpecGate delivery handoff`
with state, missing, `next_actor`, `next_command`, and the same
acceptance-criteria summary and lines; never success wording. Report a failed
status as failure. Echo `accepted` without claiming this agent accepted it.

Full mode: use the URL from
`specgate open "$WORK_REF" --print --json`. In Local mode, never call `open`.
Never reread the completion file, call stats or audit, or claim cleanup
eligibility, bugs prevented, time saved, accepted, or delivered without
authoritative status.
