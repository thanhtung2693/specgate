# How SpecGate works

SpecGate records the work you approved, gives the coding agent focused context,
and stores the evidence and human decision in a Local workspace.

Your existing tools stay in charge of their jobs. Keep authoring in OpenSpec,
Spec Kit, Superpowers, Markdown, or another format. Keep using your tracker, IDE,
pull requests, and CI. SpecGate connects those steps without trying to replace
them.

## Why the handoff needs its own record

AI-assisted delivery can move faster than team memory. An agent may start from
a chat summary, stale tracker text, an old spec file, or an unreviewed prompt.
SpecGate makes the handoff answerable:

- which artifact version is authoritative;
- which policy applies;
- which readiness gates ran;
- which Context Pack the agent used;
- which evidence supports delivery;
- which review verdict closed or reopened the loop.

## Who does what

| Actor or tool | Role |
|---|---|
| Authoring tool | creates specs, plans, designs, or other artifact documents |
| SpecGate | snapshots artifacts, runs Local checks, creates Context Packs, records evidence and decisions |
| Human reviewer | approves scope, then accepts delivery or requests changes against the exact review |
| Coding agent | implements the approved scope in your IDE, runs project checks, and submits evidence |

## The delivery loop

```text
artifact package
→ governance policy
→ readiness checks
→ human approval
→ Context Pack
→ implementation
→ delivery evidence
→ human decision on the exact review
```

The Local record lets you return later to the approved context, per-criterion
evidence, and the decision it supported.

## Quick and artifact-backed routes

SpecGate supports two common routes.

Use the **quick route** for small, well-understood
changes. Your agent drafts the title and acceptance criteria for your approval;
`work context` then derives a lightweight brief from the persisted work item.
The agent still returns delivery evidence.

Use the **artifact-backed route** when work needs a reviewed spec, design,
plan, or other documents. SpecGate snapshots the package, checks readiness,
and records human approval. The work item points back to that exact version.
Publish and approve a new version before new work uses edits to the source.

Both routes require the agent to report against the stored criteria. For a
concrete skipped check and repair, see [Try a delivery evidence gap](../guides/try-delivery-proof.md).

## The Change facade

For an existing handoff, `specgate change status <work-ref>` is a compact view
of where delivery stands and who should act next. It brings evidence, assurance,
human decision, recorded Git receipt, freshness, missing requirements, and the
next command into one readback so a passing automated check is not mistaken for
human acceptance.

The facade does not create a new durable Change entity. It is a task-oriented
view over the existing work, artifact, gate, and delivery records. `change
submit` continues the existing delivery tail; `change accept` and `change
request-changes` record the existing human decision. The detailed command
families remain available when the compact view is not enough: `work`,
`artifact`, `gates`, `delivery`, `audit`, and `verify`.

Preparation and approval orchestration deliberately remain outside this slice:
there is no `change prepare` or snapshot-approval orchestration. That boundary
keeps the facade focused on the implemented post-handoff loop rather than
implying a new lifecycle or storage model.

## Why the CLI is the main interface

The CLI gives people, scripts, and coding agents the same workflow. It:

- initializes Local stores;
- stores selected user and workspace;
- publishes and reads artifacts;
- lists work needing attention;
- returns Context Packs;
- scaffolds completion reports;
- submits delivery evidence;
- diagnoses compatibility.

Use the optional browser UI to review approved context, criterion evidence
and the exact human decision.

## Related

- [Quickstart](../quickstart.md)
- [Artifacts and Context Packs](artifacts-and-context-packs.md)
- [Governance and gates](governance-and-gates.md)
