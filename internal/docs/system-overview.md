# System Overview

## What SpecGate is

SpecGate is the governance layer between IDE agents and issue trackers.

```
Any spec format in.  Approved context out.  Implementation evidence back.
```

It does not generate specs or execute code. IDE agents own creation and execution. SpecGate owns **governance and organizational memory** — the part those agents do not naturally own.

## Three layers

| Layer | Tools | Role |
|---|---|---|
| **IDE agents** | Cursor, Claude Code, Codex, OpenSpec, Spec Kit | Author, research, refine, and execute specs |
| **SpecGate** | Doc Registry · Agents · UI | Register, version, review, approve, hand off, reconcile |
| **Issue trackers** | Linear, GitLab, GitHub | Track work; send implementation evidence back |

## The workflow

1. The IDE agent authors a spec in whatever format it prefers.
2. The agent publishes the spec to SpecGate via the `specgate` CLI.
3. SpecGate versions the package, records source and lineage, and runs readiness gates.
4. A human reviews the spec in the SpecGate UI — approves or requests changes.
5. The approved version becomes canonical.
6. The IDE agent pulls the approved Context Pack via the `specgate` CLI and begins building.
7. During implementation the agent reports ambiguities, deviations, and completion evidence via the `specgate` CLI.
8. Git webhooks (PR/MR merged, CI results) and tracker webhooks arrive independently.
9. SpecGate's delivery review judges the reported evidence against each acceptance criterion and records a verdict; failed criteria carry into the next Context Pack.
10. When the canonical spec needs updating, someone opens an artifact-update proposal (a reviewer requesting changes, a manual edit, or an agent via `draft_artifact_update`); a human approves it, and approval materializes the new version.

## What is fixed vs flexible

**Flexible:** document set, filenames, headings, authoring tool. Any spec format works.

**Fixed:** the governance envelope — work item / feature linkage, version, status, source and lineage, document-to-role mapping, canonical flag, approvals, gates profile. The envelope is stable regardless of what's inside it.

## Governance profiles

A profile is the governance bar for a kind of change. It defines which document roles are required, which readiness gates run, and what evidence the delivery verdict needs. Built-in profiles:

| Profile | Ceremony |
|---|---|
| Generic change | Spec + plan; light gates |
| High-impact feature | Spec + design + plan + verification; full gates; corroborated evidence required |
| Bug fix | Spec (problem + AC) + verification; minimal gates; self-approve allowed |
| ADR | Spec (decision + context) + research; decision-record gates |
| Research spike | Spec (question) + research; no plan required |

Profiles are keyed by change-type, not by authoring tool. The `source` field (OpenSpec, Spec Kit, Cursor, etc.) is lineage only — it carries no governance meaning.

## Where LLMs are used

**Generative model (governance compute — in agents):**
- Readiness gates — 6 judges + completeness check (locate the minimum executable contract)
- Delivery review — verify acceptance criteria against implementation evidence
- Route classifier — route full vs. quick ceremony
- Context Pack and quick work-item compilation

**Embeddings model (in doc-registry):**
- Knowledge search — surface relevant documents during review and handoff

**Deterministic (no model):**
- Artifact versioning, lifecycle transitions, event log
- Context Pack compilation
- Human approval, canonical promotion
- Webhook intake and evidence recording
