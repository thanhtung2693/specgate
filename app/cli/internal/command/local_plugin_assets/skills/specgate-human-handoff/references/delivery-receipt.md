# Delivery receipt format

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
