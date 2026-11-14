# SpecGate promotional video design

This silent 15.4-second product tour follows one Local delivery example in a
persistent evidence view, from a coding agent's claim to a developer's decision.

## Style

- Near-black canvas, warm charcoal product surface, and fine borders.
- SpecGate indigo marks the initial claim and pending human decision. Amber
  marks missing evidence. Green marks an observed passing test and the recorded
  acceptance.
- Host Grotesk for narrative and body copy. Commit Mono for the status and
  action labels.
- A single evidence rail carries the story. The headline and action label
  transition in place, and the acceptance card overlays the same surface.

## Story and timing

| Time | What the viewer sees |
| --- | --- |
| 0.0–2.2s | The agent reports that the health check meets its criteria. |
| 2.2–6.0s | The Local CLI reads the configured report. The required 503 testcase is missing even though the suite ran. |
| 6.0–9.9s | The coding agent adds the unit test and reruns the checks. The CLI observes the selected test pass. |
| 9.9–12.5s | The developer reviews the changed test and its result. |
| 12.5–15.4s | The developer accepts the exact review. SpecGate records the decision with its evidence. |

The health check is an illustration of evidence from a configured unit test.
The agent owns the code and test changes. The developer decides whether to
accept the delivery.
