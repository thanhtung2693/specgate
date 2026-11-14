---
name: specgate-work-delivery
description: Use when implementing, resuming, verifying, or reworking an approved SpecGate work item or Context Pack, or when SpecGate status names the implementing agent.
---

# Delivering Work

Apply the [SpecGate operating contract](../specgate/SKILL.md#operating-contract).
Implement approved scope; stop at next actor; never decide for humans.

## 1. Load the exact contract

Read scope/criteria, verification, document index, and status:

```bash
specgate work resume "$WORK_REF" --json
```

Pins are optional; for one, show work/digest/bindings/literal command/cwd.
They freeze commands/cwd, exact selectors, and watched paths;
scripts remain mutable/untrusted and shell is unsandboxed. JUnit selectors are exact;
`--run-checks` observes a fresh ignored report, not a trustworthy runner.

Preserve drafts. Choose an unused work-specific `$CHECKS_PATH` under
`.specgate/work` with `context_digest`, `shell: "sh"`, and
`checks: [{name, command, cwd}]`; every bound name once. Run:

```bash
specgate work verification "$WORK_REF" --file "$CHECKS_PATH" --dry-run --json
```

Dry-run neither pins nor executes. After successful preview and explicit human
confirmation of commands/consequences, rerun that file without `--dry-run`, with
`--yes`. Reuse response. On conflict, preserve history and report the error.

Use `data.status`; read pinned scope and non-goals:

```bash
specgate work context "$WORK_REF" --document "$DOCUMENT_PATH" --role "$ROLE" --json
```

Copy path/role from `data.documents`. Reuse matching-digest content; the
index cannot replace it. Quick work uses persisted scope/criteria.

Stop for missing approval/criteria, placeholder criteria, or stale context.
Missing approved artifact: hand to human or `specgate-work-preparation`.

Reuse results until state changes; avoid duplicate reads. Follow status:

- If `data.next_actor` is not `implementing_agent`, hand off without editing.
- For `review_pending`, accept only one `specgate` `next_command` without shell
  operators; else hand off. Run it verbatim before sections 2–5. On failure, go
  to section 7. On success, reread Change status; stop if still `review_pending`.
- For `rework_requested`, carry `guidance` and `missing` through: focused fix,
  affected checks, updated evidence, submit, fresh status. Never submit old
  evidence first.
- Otherwise record `missing` and `guidance`, then follow `next_command`'s named
  section or stop if none matches.

Map every change/check to a criterion or required repository-doc update.

## 2. Resume safely

Checkpoints need human request; resume is read-only.

For `next_actor=implementing_agent` in `implementation` or `rework_requested`,
inspect edits, rerun checks, submit fresh evidence. Otherwise require
`freshness`; stop if unavailable.

For a machine handoff, let the user review the entire-store scope from
`specgate transfer prepare`. Receive into a new directory and let the user
confirm activation for the receiving Git checkout. After activation, use
the returned `resume_command` as written, including any `--since`
checkpoint. Its source observations are historical. Preserve pinned check
commands and paths, and check destination tools and paths before running
anything. Follow the returned `next_actor` and `next_command` for human
decisions. Git working files and external evidence travel separately.

## 3. Check artifact drift

Local `.specgate/rubrics/` adds project instructions at publication. Judge frozen tasks, never current files.
Authorized rubric edits require preparation preview/new publication; existing work keeps its policy.

Skip when no artifact exists. Otherwise dispatch tasks and follow `spec_repo_drift`'s frozen `skill_content`:

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

Preserve scope/non-goals. Record required tests, lint, types, builds, and skip
reasons. Snapshot changes belong to `specgate-work-preparation`.

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
`criteria[]` entry per canonical criterion. Each claim must be independently
reviewable. Anchor evidence with `line` or a verbatim `heading`; a path is
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

For `implementing_agent`, complete `missing`, follow `next_command`, then refresh
status. Scaffolding is not completion. Hand off `next_command` verbatim.

For `human_reviewer`, `maintainer`, or `none`, stop; `awaiting_review` belongs to
humans. Only when the human explicitly requests it, use a different review-only agent:

```bash
specgate delivery peer-review "$WORK_REF" --init --json
specgate delivery peer-review "$WORK_REF" \
  --file "$PEER_REVIEW_PATH" --json
```

Keep `data.path` as `$PEER_REVIEW_PATH`; same existing-file rule.

Completion criterion: evidence awaits human review; never run human-decision commands.

Export review requests only on human request:

```bash
specgate delivery handoff export "$WORK_REF" --json
```

Report `data.path` to commit, and `data.git_ignored` when true — an ignored
bundle never reaches the reviewer. Never commit or decide.

## 7. Show the delivery handoff

Read and follow [the delivery receipt format](../specgate-human-handoff/references/delivery-receipt.md)
using the latest authoritative status. Use `specgate-human-handoff` for the
exact CLI next action and optional Local browser review. Never reread the
completion file, call stats or audit, or claim cleanup eligibility, bugs
prevented, time saved, accepted, or delivered without authoritative status.
