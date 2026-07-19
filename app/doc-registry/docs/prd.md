# PRD - Doc Registry

**Status:** current
**Document type:** product intent

## 1. Purpose

Doc Registry is SpecGate's durable source of truth for governed work. It stores
approved and draft artifacts, work items, delivery evidence, Knowledge context,
and integration signals so agents, UI, CLI, and reviewers all read the same
contract.

## 2. Problem

SpecGate needs one trusted registry because multi-agent delivery fails when:

- agents read different artifact versions;
- quick work, full artifacts, and delivery evidence live in separate places;
- stale Knowledge or tracker state is invisible during handoff;
- delivery review cannot prove what passed, failed, or needs human review;
- audit history is spread across chat, local files, and provider systems.

## 3. Goals

- Preserve immutable artifact versions and expose the current approved contract.
- Keep workboard state durable while deriving phase from evidence and artifact
  pointers.
- Make Context Packs reproducible from stored artifacts, acceptance criteria,
  Knowledge provenance, and unresolved gate feedback.
- Store delivery-review outcomes, human decisions, and corroborating evidence.
- Provide internal REST surfaces for CLI, IDE agents, UI, and agents service.
- Keep retention and cleanup conservative: never delete active governed work.

## 4. Non-goals

- Public authentication/RBAC at the Doc Registry HTTP layer.
- Model reasoning or drafting ownership; agents service and IDE agents own that.
- Replacing Git, GitHub, GitLab, Linear, or the SpecGate UI.
- Acting as a generic document manager outside governed SDLC work.

## 5. Users and Consumers

| Actor | Need |
| --- | --- |
| Product/governance operator | Review artifacts, settings, Knowledge, and work state |
| Tech lead/reviewer | Approve artifacts and delivery decisions |
| CLI user | Create/resolve/archive work and submit delivery evidence |
| IDE coding agent | Read Context Packs, Skills, and delivery review gaps |
| Agents service | Run readiness, quick-work creation, delivery review |
| UI | Render work queues, artifacts, settings, integrations, and verification |
| Provider webhooks | Record selected-resource PR/MR observations, optional Linear work-tracking signals, and scope-drift signals |

## 6. Product Principles

- **Registry, not orchestrator:** persist contracts and evidence; do not hide
  workflow decisions in background magic.
- **Human-clear delivery:** human decisions outrank platform reruns for the same
  completion; corrected completions start a new decision cycle, and only human
  approval may archive work or close linked tracker issues.
- **No invented work:** featureless quick work remains featureless unless the
  caller chooses a Feature.
- **No silent samples:** live-mode UI/API responses must not fall back to bundled
  demo rows.
- **Internal trust:** actor fields are audit attribution unless a route states a
  cooperative guard.

## 7. Core Outcomes

- A reviewer can tell which artifact version is approved and why.
- A coding agent can pick up one Context Pack and see current instructions,
  Knowledge provenance, gate gaps, and delivery-review feedback.
- A delivery reviewer can distinguish agent claims, grounded evidence,
  repository-observation webhook evidence, peer review, deterministic check
  bindings, and human decisions.
- Operators can clean expired artifacts, fixed demo seeds, and archived work
  without deleting approved artifacts, active Features, in-flight work, or audit
  events.

## 8. Success Metrics

| Metric | Target |
| --- | --- |
| Approved artifacts accidentally deleted by cleanup | 0 |
| Live UI surfaces populated by demo/sample data | 0 |
| Delivery pass without authoritative review state | 0 |
| Context Pack assembly from approved artifacts | P99 under 1s local stack |
| Work resolution by CR key/id or tracker ref | 99%+ for linked work |

## 9. Risks

| Risk | Mitigation |
| --- | --- |
| Spec/docs drift from route code | Keep route catalog in spec and verify with release-readiness/doc tests |
| Pre-release compatibility paths accumulate | Do not add them; delete unreachable callers and validate only the collapsed fresh-install schema |
| Internal endpoints exposed publicly | Deploy behind trusted network/proxy; expose only needed Bearer-gated paths |
| Evidence trust becomes ambiguous | Persist executor, actor, source, trust tier, and human decision metadata |
