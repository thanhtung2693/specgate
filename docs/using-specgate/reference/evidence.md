# Evidence reference

Delivery evidence tells SpecGate what changed and why the acceptance criteria
should be considered satisfied.

## Completion report

Create a scaffold:

```bash
specgate delivery report <work-ref> --init
```

The scaffold includes one `criteria[]` entry per acceptance criterion.

Typical shape:

```json
{
  "change_request_id": "CR-123",
  "event_type": "coding_agent.completed",
  "severity": "info",
  "summary": "Implemented the healthcheck endpoint.",
  "agent": {
    "name": "codex"
  },
  "checks": [
    {
      "name": "tests",
      "status": "pass",
      "command": "go test ./internal/health -count=1",
      "detail": "go test ./internal/health -count=1"
    }
  ],
  "criteria": [
    {
      "criterion_id": "ac-1",
      "text": "GET /healthz returns 200 when the service is up",
      "claim": "satisfied",
      "evidence": {
        "kind": "test",
        "path": "internal/health/handler_test.go"
      }
    }
  ],
  "affected_files": [
    "internal/health/handler.go",
    "internal/health/handler_test.go"
  ]
}
```

## Field notes

| Field | Purpose |
|---|---|
| `change_request_id` | durable work identity populated by the scaffold command; verify it before reusing an existing scaffold |
| `agent.name` | required stable name of the coding agent submitting this completion |
| `summary` | concise delivery summary |
| `checks[]` | tests, builds, lint, type checks, manual checks |
| `criteria[]` | per-acceptance-criterion claim and evidence |
| `affected_files[]` | files changed by the implementation |
| `severity` | signal severity for feedback events |

`checks[].status` values are:

- `pass`
- `fail`
- `skipped`

Values must use this exact wire contract; aliases such as `passed`, `failed`,
and `skip` are rejected.

`claim` values are:

- `satisfied`
- `partial`
- `not_done`

Claims must use these exact values.

Each `criteria[].evidence` value is an object. Use `kind` plus a local `path`
when the proof is in the checkout; optional `line`, `heading`, `url`, and
`file_key` make a citation more precise. The CLI verifies local evidence paths
and records a digest plus a `grounding.status` unless `--skip-evidence-check` is
explicitly used. An excerpt accompanies the digest only when `line` or a
`heading` actually present in the file anchors the citation; otherwise the
status is `heading_not_found`, `line_out_of_range`, or `unanchored` and a
reviewer can see that the path alone was cited.

Those field names are the whole contract: `kind`, `path`, `line`, `heading`,
`url`, `file_key`, and the CLI-stamped `grounding`. Any other field is rejected
before submission in both modes, so a completion accepted in Local mode is not
refused later by a Full appliance.

## Bound criteria

An acceptance criterion ending in `@check:<name>` is verified from the named
`checks[]` row rather than from its prose claim, in both Local and Full mode.
The stored acceptance criterion is the authority for the binding, so a
completion cannot escape enforcement by omitting `verification_binding`.

| Bound check | Result |
|---|---|
| `pass` | criterion met |
| `fail` | criterion unmet |
| `skipped` or absent from `checks[]` | not verifiable — Full mode returns `needs_human_review`, Local mode fails the review and names the criterion and check |

Enforcement covers a missing or skipped check. It does not detect a falsely
claimed `pass`: `checks[].status` is agent-reported unless the submission ran
with `change submit --run-checks`, which re-executes each non-skipped command
and records the superseded value in `checks[].claimed_status`.

## Evidence quality

Good evidence is concrete:

- command output;
- test names;
- API response details;
- UI behavior;
- file paths;
- PR, commit, or CI links;
- screenshot or recording references when visual behavior matters.

Weak evidence is vague:

- "done";
- "looks good";
- "tests pass" without naming the command;
- a summary that does not mention acceptance criteria.

## Rework loop

If delivery review fails:

1. read the failed criterion or gate hint;
2. fix the smallest named gap;
3. rerun relevant checks;
4. update the completion report;
5. run `specgate delivery submit` again.

## Related

- [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md)
- [Governance and gates](../concepts/governance-and-gates.md)
- [CLI reference](cli.md)
