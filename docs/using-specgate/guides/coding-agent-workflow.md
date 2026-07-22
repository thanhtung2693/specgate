# Use SpecGate with a coding agent

Use this guide after you have approved a SpecGate work item. Give its work
reference to your coding agent; the installed SpecGate skill will read the
approved contract, keep the implementation in scope, submit evidence, and stop
for your final decision.

SpecGate works with your existing spec framework. Superpowers, OpenSpec, Spec
Kit, and other authoring tools keep their own file locations and Git behavior.
SpecGate registers those documents in place; `.specgate/` is for SpecGate state
and generated work receipts, not a replacement home for framework documents.

## Before you start

Install the CLI and IDE plugin at the scope that fits your project, then verify
both layers:

```bash
specgate doctor
specgate plugins doctor
```

The plugin may be user-global or project-local. Follow [Install SpecGate in your
coding IDE](install-ide-plugins.md) for the exact install and doctor commands.
The IDE executable itself does not need to be on `PATH`; the plugin doctor
checks the managed SpecGate files.

The work item must already have human approval and acceptance criteria. If you
are still publishing or reviewing a specification, follow [Use the SpecGate
CLI](cli-workflow.md) first.

## Give the work to the agent

Start a fresh IDE task with the stable work reference:

```text
Implement SpecGate work CR-123. Follow the installed SpecGate skills. Stop for
any human decision.
```

That is enough. You do not need to paste the specification or a long command
sequence into the prompt.

## What the agent does

### 1. Read the approved contract

The agent starts from the Context Pack:

```bash
specgate work show <work-ref> --json
specgate work context <work-ref> --json
specgate change status <work-ref> --json
```

The Context Pack contains the approved scope, acceptance criteria, non-goals,
risks, and artifact references. It outranks chat history and stale tracker or
repository notes. The agent stops when approval is absent, the pack is stale,
or the contract is ambiguous. It also stops without editing when Change status
names a human reviewer, maintainer, or no next actor.

### 2. Implement and verify

The agent maps each change to an acceptance criterion or required repository
documentation update, follows the repository's contributor rules, and records
the checks it actually ran. It does not silently expand scope or mutate an
approved artifact. A material spec change returns to artifact preparation and
human approval as a new version.

### 3. Submit criterion-level evidence

The agent scaffolds a completion report, fills one claim for every acceptance
criterion, and submits it:

```bash
specgate delivery report <work-ref> --init --json
COMPLETION_PATH="<exact data.path from the preceding response>"
specgate change submit <work-ref> \
  --file "$COMPLETION_PATH" \
  --run-checks --yes --json
specgate change status <work-ref> --json
```

The agent uses the scaffold command's returned `data.path`; it does not build a
filename from the work reference. If a regular scaffold already exists, the CLI
refuses to overwrite it and returns its exact `error.details.path`. The agent
reuses that file only after confirming it belongs to the same work and Context
Pack.

`--run-checks` executes the commands stored in the completion report, so the
agent reviews that file before authorizing execution. Each claim points to
reviewable evidence such as a test assertion, source line, API response, or UI
observation. A skipped or failed check cannot support a satisfied claim.

### 4. Add an independent review only when you request one

If you want another agent's evidence before deciding, ask a different
review-only agent to inspect the same Context Pack, checkout, checks, and latest
completion receipt:

```bash
specgate delivery peer-review <work-ref> --init --json
PEER_REVIEW_PATH="<exact data.path from the preceding response>"
specgate delivery peer-review <work-ref> \
  --file "$PEER_REVIEW_PATH" --json
```

Peer review strengthens the evidence but never approves or accepts the work.
It is bound to the latest submitted Git receipt and becomes stale after a new
completion. It also does not override `change status`: when status names a human
as `next_actor`, the implementing agent stops for that human.

### 5. Return the delivery handoff

Immediately before its final response, the agent reads a fresh
`specgate change status <work-ref> --json`. When the state is exactly
`awaiting_acceptance`, it returns a compact per-work governance handoff:

```text
SpecGate delivery receipt — Add health endpoint (CR-123)
Evidence: Ready for human review
Assurance: Agent-reported; locally reproduced; second agent affirmed
Decision: Awaiting human acceptance
Receipt: commit a1b2c3d
Freshness: The stored receipt was not checked against the current checkout.
Next (human_reviewer): specgate --yes change accept CR-123
```

The receipt means the work is awaiting your acceptance, not accepted or
delivered. It does not prove that SpecGate prevented bugs or saved time. A
stale warning does not rewrite the reported state; the exact reason appears on
a separate `Stale:` line so you can judge it before deciding.

Other states produce a delivery handoff with the blocker, missing evidence,
next actor, and exact next command rather than success language.

## Make the final decision

Review the implementation and receipt, then run exactly one human decision:

```bash
specgate --yes change accept <work-ref>
specgate --yes change request-changes <work-ref> --note "<focused feedback>"
```

In Local mode, the handoff uses the work title, stable ID, and exact CLI
command. There is no browser UI. In Full mode, the agent may also show the URL
returned by `specgate open <work-ref> --print --json`; it never constructs a
URL itself.

If you request changes, give one focused reason. The agent fixes that gap,
reruns the affected verification, submits a new completion, and starts a new
review cycle.

## Check whether a published spec was delivered

To verify that one immutable published spec has corresponding work and delivery
state, inspect that exact version:

```bash
specgate artifact coverage <artifact-id>
```

Artifact coverage is exact-version evidence. Do not infer coverage from a
filename, feature name, latest version, or chat history.

## Local and Full behavior

| Concern | Local mode | Full mode |
| --- | --- | --- |
| Contract and delivery evidence | Local SpecGate store | Selected server workspace |
| IDE semantic readiness | Frozen tasks completed by the IDE agent | Same task loop when dispatched to the IDE |
| Final handoff | Stable ID and CLI command | Stable ID, CLI command, and returned UI URL when available |
| Human authority | Explicit CLI decision | CLI or UI decision |
| Server-only policy and model operations | Unavailable | Available when configured |

The agent determines the mode from `specgate doctor --json`; it does not infer
it from Docker, URLs, or browser availability.

## Related

- [Use the SpecGate CLI](cli-workflow.md)
- [Artifacts and Context Packs](../concepts/artifacts-and-context-packs.md)
- [Evidence reference](../reference/evidence.md)
- [CLI reference](../reference/cli.md)
