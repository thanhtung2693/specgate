---
name: using-specgate
description: SpecGate router. Use when a task mentions SpecGate, the specgate CLI, work items, artifacts, quality/readiness gates, Context Packs, implementation feedback, or delivery review.
---

# Using SpecGate

## Invocation

Invocation mode: router. Load this skill when SpecGate is mentioned, then select
one focused phase skill before acting. Keep this file small; it is the index,
not the workflow body. Load a phase skill directly only when the user request
already names that phase; return here when the task crosses phases.

Select the narrowest phase (each skill's frontmatter carries its triggers):

- `setting-up-specgate-project` — onboarding SpecGate to a repository.
- `preparing-work` — shaping, publishing, and readiness before approval.
- `delivering-work` — pickup through implementation, evidence, and review.

Use the `specgate` CLI for platform interactions. Run `specgate --help` when
the required command is unclear.

If the user only asks a SpecGate concept, audit, or troubleshooting question,
answer it from this router without forcing a lifecycle phase. Do not edit the
target repository or SpecGate records unless a phase skill requires it.

Hard stops:

- A readiness pass is not human approval.
- A Context Pack outranks chat history, tracker comments, and stale repo docs.
- Do not silently change approved scope.

Completion criterion: before leaving this router, one phase skill is selected
or the task is explicitly identified as a non-lifecycle SpecGate question.
