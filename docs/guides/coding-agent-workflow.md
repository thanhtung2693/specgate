# Use SpecGate with a coding agent

This workflow keeps implementation tied to approved context and returns
criterion-level evidence after the code changes.

Install the IDE integration first:
[Install SpecGate in your coding IDE](install-ide-plugins.md).

## Before the agent edits code

A work item is ready for implementation only when its required governance
conditions are satisfied:

- the intended artifact or quick Context Pack exists;
- required readiness gates have acceptable results;
- required human approval exists;
- acceptance criteria and scope are available;
- the Context Pack is not stale.

Readiness is not approval. A green deterministic check alone does not authorize
implementation.

## 1. Resolve the work item

```bash
specgate status --json
specgate work show "$WORK_REF" --json
```

`$WORK_REF` may be a SpecGate change-request ID, work-item key, tracker key, or
supported issue URL.

## 2. Check governance state

```bash
specgate gates status "$WORK_REF" --json
specgate work policy "$WORK_REF" --json
```

Stop before editing when required approval is missing. Surface failed,
unresolved, and not-run gates instead of treating them as passed.

## 3. Read the Context Pack

```bash
specgate work context "$WORK_REF" --json
```

Read:

- approved scope and non-goals;
- acceptance criteria;
- risks and constraints;
- design references;
- verification requirements;
- governance warnings;
- source artifact and version.

Treat approved Context Pack content as stronger than chat history, tracker
comments, or stale repository documents.

## 4. Fetch additional artifact files

When the pack points to a richer artifact:

```bash
specgate artifact show "$ARTIFACT_ID" --json
specgate artifact files "$ARTIFACT_ID" spec.md tasks_fe.md --json
```

The file command returns path, size, and URL references by default. Fetch only
the files needed for the task, and add `--content` only when the file body is
required. Preserve explicit non-goals and blast-radius limits.

## 5. Implement within scope

The coding agent:

- changes code and tests in the repository;
- uses the repository’s development rules;
- runs verification proportional to the change;
- keeps repository-owned docs aligned with behavior;
- does not silently expand approved product scope.

SpecGate does not need general Git read access. Repository work stays with the
coding agent.

## 6. Report documentation updates or ambiguity

When implementation changes repository-owned system documentation, report the
signal with a JSON feedback body:

```bash
specgate delivery report "$WORK_REF" --file docs-updated.json --json
```

When approved intent is ambiguous or contradictory:

1. report `coding_agent.blocked_ambiguity`;
2. ask the human for the missing decision;
3. propose a governed artifact revision if the source of truth must change.

```bash
specgate delivery report "$WORK_REF" --file ambiguity.json --json
specgate artifact propose "$ARTIFACT_ID" --file proposal.json --json
```

Do not edit approved artifact state directly.

## 7. Report completion and AC evidence

Scaffold the completion report — it prefills `event_type`, and one `criteria[]`
entry per acceptance criterion with the correct `criterion_id` and text:

```bash
specgate delivery report "$WORK_REF" --init --json
```

Fill in:

- summary;
- affected files;
- automated checks and skipped-check reasons;
- one claim per acceptance criterion;
- specific evidence (`{kind, path, ...}`).

Criterion claims are:

- `satisfied`;
- `partial`;
- `not_done`.

Do not claim `satisfied` when the check was not run or evidence is unclear.

## 8. Submit the delivery tail

One command reports completion, runs gates, triggers delivery review, and
returns the per-criterion verdict in a single
`{report, gates, review, status}` envelope:

```bash
specgate delivery submit "$WORK_REF" --file completion.json --json
```

If a stage fails, the command stops and the error envelope names the failing
stage in `error.details.stage`.

The individual commands are available when you need one stage at a time
(none of them require `--yes` in non-interactive runs):

```bash
specgate delivery report "$WORK_REF" --file completion.json --json
specgate gates run "$WORK_REF" --json
specgate delivery review "$WORK_REF" --json
specgate delivery status "$WORK_REF" --detail --json
```

The delivery reviewer compares ACs with the latest completion report and
available corroboration. Stronger governance may require evidence beyond the
builder’s own claim.

## 9. Repeat when review fails

When `delivery submit` (or `delivery status`) returns a failed or unclear
verdict:

1. read `outstanding_md` and per-criterion findings;
2. fix only the identified gap;
3. rerun relevant repository verification;
4. update completion.json and run `delivery submit` again.

Finish when review passes or a genuine external blocker is recorded clearly.

## Governance-ops MCP boundary

Coding agents use the CLI. SpecGate’s governance-ops agent retains an internal
MCP boundary for its bounded tool calls and governance-ops.

The separation keeps IDE setup simple:

| Consumer | Interface |
|---|---|
| Claude Code, Cursor, Codex, scripts | `specgate` CLI |
| Governance-ops service | internal MCP and service APIs |
| PM, QC, tech lead, operator | SpecGate and CLI |

## Continue

- [Artifacts and Context Packs](../concepts/artifacts-and-context-packs.md)
- [CLI reference](../reference/cli.md)
