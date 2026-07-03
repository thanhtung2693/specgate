# How SpecGate works

SpecGate is a governance layer between approved intent and coding agents. It
does not replace your spec tool, tracker, IDE, pull request process, or CI. It
records which artifact version was approved, what context the coding agent
received, and what evidence came back after implementation.

## The problem

AI-assisted delivery can move faster than team memory. Work may start from a
chat summary, stale tracker text, an old spec file, or an unreviewed prompt.
SpecGate makes the handoff explicit:

- which artifact version is authoritative;
- which policy applies;
- which readiness gates ran;
- which Context Pack the agent used;
- which evidence supports delivery;
- which review verdict closed or reopened the loop.

## Actors and tools

| Actor or tool | Role |
|---|---|
| Authoring tool | creates specs, plans, designs, or other artifact documents |
| SpecGate | stores artifacts, resolves policy, gates readiness, creates Context Packs, records evidence |
| Human reviewer | approves the artifact version or requests changes |
| Coding agent | implements only from the approved Context Pack |
| CI / integrations | provide corroborating delivery evidence when connected |

## Delivery loop

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

Each step leaves durable state. Later reviewers can see why a work item was
ready, what the agent received, and whether delivery matched acceptance
criteria.

## Quick route and full route

SpecGate supports two common routes.

The **quick route** is for small, understood changes. A user creates a work item
with a title and acceptance criteria. SpecGate creates a lightweight Context
Pack and still requires delivery evidence.

The **full route** is for larger changes. A versioned artifact package is
published, checked, reviewed, and approved. Work items then use the approved
artifact as their source of truth.

Both routes end in delivery review.

## What the CLI does

The CLI is the stable interface for users, automation, and coding agents. It:

- initializes local deployments;
- stores selected user and workspace;
- publishes and reads artifacts;
- lists work needing attention;
- returns Context Packs;
- scaffolds completion reports;
- submits delivery evidence;
- diagnoses compatibility.

The web UI is available for review, inspection, settings, governance chat, and
workflow scanning, but the alpha path is CLI-first.

## Where MCP fits

Doc Registry exposes MCP tools for embedded integrations. The normal
human/agent workflow should still prefer the CLI because it handles output
modes, config, workspace selection, and delivery scaffolding consistently.

## What SpecGate does not do

SpecGate does not:

- author every spec for you;
- replace human approval;
- replace pull request review or CI;
- enforce production authorization in the alpha local stack;
- guarantee quality without clear acceptance criteria and evidence.

It makes the governed handoff and delivery review observable.

## Related

- [Quickstart](../quickstart.md)
- [Artifacts and Context Packs](artifacts-and-context-packs.md)
- [Governance and gates](governance-and-gates.md)
