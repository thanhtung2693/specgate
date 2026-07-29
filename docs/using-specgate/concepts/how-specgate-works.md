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

## Quick and artifact-backed routes

SpecGate supports two common routes.

In Local and Full modes, use the **quick route** for small, well-understood
changes. Your agent drafts the title and acceptance criteria for your approval;
`work context` then derives a lightweight brief from the persisted work item.
The agent still returns delivery evidence.

### The quick route, as a conversation

Three roles: **you**, your **agent**, and **SpecGate**. Your part is three
moments — ask, approve, accept.

> **You:** Add tagging to notes.
>
> **Agent:** Here's the work order — "Tag notes and filter by one tag", four
> acceptance criteria you can observe, and out of scope: renaming tags,
> autocomplete. Approve?
>
> **You:** Drop autocomplete from criteria 4, otherwise yes.
>
> **SpecGate:** Approved checklist locked. From here on, the agent can't edit
> what it will be graded against.
>
> **Agent:** *implements, writes tests, runs them…* Done. SpecGate handed me
> a report where every criterion starts as **not done** — I claim each one
> with its evidence, and anything I stay silent about counts as unfinished.
> Four claims, four pieces of evidence, submitting.
>
> **SpecGate:** Rechecking the report, not the vibes. I reran the tests the
> agent says it ran: three reproduce — **observed**, pass. The fourth claimed
> *pass* but fails when I run it — **corrected on the record**. Back to the
> agent, failure named.
>
> **Agent:** *fixes, resubmits…*
>
> **SpecGate:** Fresh review — an old pass is never reused for new code. All
> four criteria observed. Your decision.
>
> **You:** Accept.
>
> **SpecGate:** Accepted, recorded under your name against this exact
> completion. Delivered.

In either mode, use the **artifact-backed route** when work needs a reviewed
spec, design, plan, or other documents. SpecGate snapshots the package, checks
it, and records human approval. The resulting work item points back to that
exact version.

### The artifact-backed route, as a conversation

Same three roles. What's new is the frozen, versioned snapshot in the middle.

> **You:** Implement the tagging spec I wrote up in docs/tagging.md.
>
> **Agent:** Read it. I'd map `docs/tagging.md` as the *spec*, and it covers
> the *plan* too, so one file carries both roles. First version for feature
> "notes-tagging". Here's the preview — nothing is saved yet. Publish?
>
> **You:** Publish.
>
> **SpecGate:** Snapshot taken — **version 1, frozen**. Edit the file all you
> like from now on; v1 won't move. Running readiness: the structural checks
> pass, and three judgment calls go to the agent, each against a fixed rubric.
>
> **Agent:** Scope is clear, the criteria are testable — but the spec has no
> verification section. That's a **warn**, and I'm submitting it as one.
>
> **SpecGate:** Readiness: warn, gap named. Your call — a warn is information,
> not a green light.
>
> **You:** I can live with that. Approve v1, with these four criteria.
>
> **SpecGate:** v1 approved and locked. Work item created, pointing at exactly
> v1, with its brief generated from the frozen copy. From here it's the same
> loop you saw above: implement, claim every criterion with evidence (silence
> counts as unfinished), recheck, your decision.
>
> *…a week later…*
>
> **You:** I changed my mind about case-sensitivity and edited the spec file.
>
> **SpecGate:** v1 hasn't moved — your edit lives only in the file. Publish it
> as **v2**, approve it, and then you choose: let the current work finish
> against v1 and open a new job for the difference, or stop it and start fresh
> against v2. Nothing migrates silently, and nothing is erased — v1, its work,
> and its receipt stay in the ledger.

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
there is no `change prepare` or snapshot-approval orchestration. That boundary
keeps the facade focused on the implemented post-handoff loop rather than
implying a new lifecycle or storage model.

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
