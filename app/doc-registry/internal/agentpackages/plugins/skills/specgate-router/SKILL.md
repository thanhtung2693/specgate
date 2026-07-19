---
name: specgate-router
description: SpecGate router. Use when a task mentions SpecGate, the specgate CLI, work items, artifacts, quality/readiness gates, Context Packs, implementation feedback, or delivery review.
---

# Using SpecGate

## Invocation

Invocation mode: router. Load this skill when SpecGate is mentioned, then select
one focused phase skill before acting. Keep this file small; it is the index,
not the workflow body. Load a phase skill directly only when the user request
already names that phase; return here when the task crosses phases.

Select the narrowest phase (each skill's frontmatter carries its triggers):

- `specgate-project-setup` — onboarding SpecGate to a repository.
- `specgate-work-preparation` — shaping, publishing, and readiness before approval.
- `specgate-work-delivery` — pickup through implementation, evidence, and review.

Use the `specgate` CLI for platform interactions. Run `specgate --help` when
the required command is unclear.

Before a mode-dependent action or handoff, run:

```bash
specgate doctor --json
```

Read `data.mode`: `local` means Local CLI; `full` means the Full
appliance or a remote server. Do not infer the mode from Docker, a configured
URL, or whether a browser happens to be available. If `doctor` cannot return a
successful envelope, report that blocker instead of guessing a handoff route.

## Advisory assistance is ephemeral

Answer governance questions from the smallest relevant CLI record:

- `work show` / `work context` — implementation contract;
- `artifact show` — exact Local content or Full metadata;
- `artifact coverage` — exact-version linked work and delivery state;
- `gates results` — stored readiness evidence;
- `delivery status` — delivery verdict and peer evidence;
- `audit` — governance history.

Advice, acceptance-criteria drafts, explanations, and summaries are ephemeral
IDE output unless an explicit CLI command persists the canonical record. Repo
reads are not Governance Knowledge; advisory output does not approve an
artifact or delivery.
For one published spec, use `artifact coverage <artifact-id>`; for all versions,
enumerate `artifact list --json` and inspect coverage for every exact id. Do not
collapse versions by feature key.

## Human-readable entity links

In Full mode, when asking for a decision or reporting lifecycle state, render
the first work item or artifact mention as a Markdown link with its
human-readable title and stable ID. Obtain the URL from SpecGate; never
construct or guess it:

```bash
specgate open "$WORK_REF" --print --json
specgate open --artifact "$ARTIFACT_ID" --print --json
```

Use the returned URL as the link destination and `<title> (<stable-id>)` as
its text. Repeated mentions in the same response may use the ID alone. If
SpecGate returns no URL, fall back to `<title> (<stable-id>)` without a link.
This applies to approval and promotion requests, blockers, handoffs, and
delivery receipts.

In Local CLI mode there is no browser UI. Do not call `specgate open` and do
not invent a URL. Render `<title> (<stable-id>)` as plain text and give the
human the exact next CLI command instead.

If the user only asks a SpecGate concept, audit, or troubleshooting question,
answer it from this router without forcing a lifecycle phase. Do not edit the
target repository or SpecGate records unless a phase skill requires it.

Hard stops:

- A readiness pass is not human approval.
- A Context Pack outranks chat history, tracker comments, and stale repo docs.
- Do not silently change approved scope.

Completion criterion: before leaving this router, one phase skill is selected
or the task is explicitly identified as a non-lifecycle SpecGate question.
