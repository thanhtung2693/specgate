# How SpecGate works

SpecGate sits between the work your team approved and the coding agent that
implements it. It records the exact approved version, gives the agent focused
context, and keeps the evidence that comes back.

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
| SpecGate | stores artifacts, resolves policy, gates readiness, creates Context Packs, records evidence |
| Human reviewer | approves the artifact version or requests changes |
| Coding agent | implements only from the approved Context Pack |
| Repository integrations | observe a submitted commit on a matching merged PR/MR |

## The delivery loop

```text
artifact package
→ governance policy
→ readiness checks
→ human approval
→ Context Pack
→ implementation
→ delivery evidence
→ delivery review
→ reconciliation or completion
```

Each step leaves a durable record. You can come back later and see why the work
was ready, what the agent received, and whether the result met the acceptance
criteria.

## Quick route and full route

SpecGate supports two common routes.

In Full mode, use the **quick route** for small, well-understood changes. Give SpecGate a title
and acceptance criteria; `work context` then derives a lightweight brief from
the persisted work item. The agent still returns delivery evidence.

In Local or Full mode, use the **full route** when work needs a reviewed spec, design, plan, or other
documents. SpecGate snapshots the package, checks it, and records human
approval. The resulting work item points back to that exact version.

Both routes end in delivery review.

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
there is no `change prepare`, no snapshot-approval orchestration, and no Local
quick-work parity yet. That boundary keeps the facade focused on the implemented
post-handoff loop rather than implying a new lifecycle or storage model.

## Why the CLI is the main interface

The CLI gives people, scripts, and coding agents the same workflow. It:

- initializes local deployments;
- stores selected user and workspace;
- publishes and reads artifacts;
- lists work needing attention;
- returns Context Packs;
- scaffolds completion reports;
- submits delivery evidence;
- diagnoses compatibility.

Use the web UI when a visual overview or human decision is more useful: reviews,
artifact inspection, settings, governance chat, and workflow scanning.

## What SpecGate does not do

SpecGate does not:

- author every spec for you;
- replace human approval;
- replace pull request review or CI;
- enforce production authorization in the v0.1 Full appliance;
- guarantee quality without clear acceptance criteria and evidence.

It gives the handoff and delivery review a reliable history.

## Related

- [Quickstart](../quickstart.md)
- [Artifacts and Context Packs](artifacts-and-context-packs.md)
- [Governance and gates](governance-and-gates.md)
