# Evidence reference

Delivery evidence flows through the completion report: the JSON body submitted
by `specgate delivery report --file completion.json` (or as the first stage of
`specgate delivery submit`). Delivery review judges each acceptance criterion
against the claims and evidence in this report and persists a per-criterion
verdict.

Scaffold a correctly-shaped report instead of hand-authoring it:

```bash
specgate delivery report "$WORK_REF" --init
```

The scaffold prefills `event_type` and one `criteria[]` entry per acceptance
criterion, using the same `criterion_id` values delivery review correlates
against.

## Completion report shape

```json
{
  "event_type": "coding_agent.completed",
  "summary": "What was implemented and how it was verified.",
  "affected_files": ["app/example/file.go"],
  "checks": [
    { "name": "tests", "status": "pass", "detail": "go test ./... (42 passed)" }
  ],
  "criteria": [
    {
      "criterion_id": "ac-0",
      "text": "The issue described is resolved and verified.",
      "claim": "satisfied",
      "evidence": { "kind": "file", "path": "app/example/file.go" }
    }
  ]
}
```

## Field notes

| Field | Notes |
|---|---|
| `event_type` | `coding_agent.completed` for a completion report. Use `coding_agent.blocked_ambiguity` (with a `summary` naming the decision needed) when blocked instead of guessing. |
| `summary` | Concrete description of what changed and how it was verified. The reviewer reads this — vague summaries earn `needs_human_review`. |
| `checks` | One entry per verification command. `status` is `pass`, `fail`, or `skipped` (with the reason in `detail`). |
| `criteria[].criterion_id` | Must match the work item's acceptance-criterion ids (e.g. `ac-0`). `--init` prefills them; delivery review's `outstanding_md` reuses them on rework. |
| `criteria[].claim` | `satisfied`, `partial`, or `not_done`. Claim honestly — the reviewer cross-checks claims against checks and summary. |
| `criteria[].evidence` | One object, not an array: `{kind, path?, line?, file_key?, heading?, url?}`. No free-form fields; put short command or file references in `path` or `url`. |

## Rework loop

If the review verdict fails, `specgate delivery status --detail` returns
`outstanding_md` naming the unmet criteria. Fix those, update the same
`criterion_id` entries, and re-run `specgate delivery submit`.

See [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md) for
the full delivery flow.
