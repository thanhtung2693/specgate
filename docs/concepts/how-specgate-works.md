# How SpecGate works

SpecGate keeps one approved version of intent between planning and
implementation, then checks delivery evidence against that intent.

## The problem SpecGate owns

AI coding tools can write and execute detailed specs. The difficult part starts
when several versions, conversations, reviewers, and delivery systems become
involved:

- Which spec version was approved?
- Is this work ready for implementation?
- What exactly should the coding agent read?
- Did implementation satisfy each acceptance criterion?
- Did the spec change after handoff?
- Which evidence came from the builder, a verifier, or an external system?

SpecGate makes those questions explicit and auditable.

## The people and tools in the loop

| Participant | Typical responsibility |
|---|---|
| PM, designer, tech lead, or authoring agent | Define intent and review artifacts |
| SpecGate | Review, approve, inspect gates, manage handoff and settings |
| `specgate` CLI | Connect users and coding agents to platform workflows |
| Coding IDE agent | Read approved context, change repository code, run verification |
| Verifier agent or human | Independently assess a gate or delivery claim |
| GitHub, GitLab, or Linear | Provide delivery and tracker signals through webhooks |
| Governance-ops service | Run bounded semantic reviews, reconciliation, summaries, and chat |

SpecGate does not replace OpenSpec, Spec Kit, Kiro, or custom authoring
workflows. It accepts their outputs as flexible document bundles and adds
versioning, policy, approval, handoff, evidence, and reconciliation.

## Working without SpecGate

Teams can build with AI coding agents without SpecGate. For small teams,
low-risk work, or early exploration, a manual workflow may be enough:

```text
Spec Kit / OpenSpec / Markdown / design docs
→ Linear, Jira, GitHub Issues, or GitLab Issues
→ Claude Code, Cursor, or Codex reads the context
→ pull request or merge request
→ CI and tests
→ human review
→ Slack, meetings, or ticket comments for clarification
→ manual spec or ticket updates
```

That workflow is valid. The cost is that important governance questions stay
manual:

- Which spec version was approved?
- Who approved it, and when?
- Which source revision or Context Pack did the coding agent use?
- Which acceptance criteria passed, failed, or stayed unclear?
- Which evidence proves each acceptance criterion?
- Was a failure caused by implementation drift or ambiguous requirements?
- Did stale knowledge or conflicting context influence the handoff?
- Was a reconciliation proposal reviewed before canonical intent changed?

SpecGate should not be positioned as "without this, teams cannot ship." Teams
can ship through discipline and convention. SpecGate becomes valuable when that
convention needs to become durable:

```text
approved artifact version
→ governed Context Pack
→ coding-agent handoff
→ evidence-backed verdict
→ reviewed reconciliation
→ audit trail
```

In short: SpecGate turns manual convention into a governed system of record for
approved handoff, evidence, verdicts, reconciliation, and audit.

## The governed delivery loop

```text
1. Publish
   An authoring tool sends a versioned artifact and impact declaration.

2. Resolve
   SpecGate chooses the effective governance level and freezes its policy snapshot.

3. Check
   Structural, semantic, evidence, or human gates assess readiness.

4. Approve
   A human approves the exact artifact version when policy requires it.

5. Hand off
   SpecGate builds a Context Pack for the coding agent.

6. Implement
   The coding agent changes code within approved scope and runs repository checks.

7. Report
   The agent reports checks and evidence for each acceptance criterion.

8. Review
   SpecGate evaluates delivery evidence and highlights unclear or failed criteria.

9. Reconcile
   Drift or new information becomes a reviewed artifact-update proposal.
```

## What the CLI is for

The `specgate` CLI supports:

- installing and operating a local deployment;
- finding and resolving work items;
- reading Context Packs and artifact files;
- publishing artifacts and running quality checks;
- reporting implementation feedback and delivery evidence;
- triggering and reading delivery review;
- authoring and activating governance packages;
- machine-readable automation through `--json`.

Coding IDE plugins call the CLI. They do not need direct access to SpecGate MCP
tools.

## Where MCP still fits

SpecGate retains MCP for the internal governance-ops agent. That agent uses a
bounded tool catalog to read artifacts, run governance operations, and answer
domain questions.

This is separate from the coding-agent interface:

| Consumer | Primary interface |
|---|---|
| Coding agents and IDE plugins | `specgate` CLI |
| Humans | SpecGate and CLI |
| Governance-ops agent | Internal MCP and service APIs |

## Quick and full routes

SpecGate supports proportional ceremony:

- **Quick route** — small, understood work such as a focused bug fix. The
  Context Pack is narrow and does not require a full PRD/spec/implementation
  bundle.
- **Full route** — features or changes needing richer product, architecture,
  implementation, QA, rollout, or risk context.

An LLM may suggest a route, but a human confirms it. Low-confidence quick
suggestions fall back to the safer full route.

The route changes preparation depth, not the delivery loop. Both routes still
require governed context, evidence, and delivery review.

## What SpecGate does not do

SpecGate does not:

- prescribe one spec file format;
- replace the coding agent;
- require platform access to a Git checkout;
- rerun every repository check itself;
- treat tracker status as proof of delivery;
- allow arbitrary custom code inside the governance server;
- provide full multi-user authentication or RBAC in the current release.

The coding agent works where the code already lives. SpecGate receives declared
results, evidence manifests, and signed webhook events.

## Continue

- [Artifacts and Context Packs](artifacts-and-context-packs.md)
- [Governance and gates](governance-and-gates.md)
- [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md)
