# Governance and gates

SpecGate keeps governance semantics fixed while letting teams customize the
checks that fit their work.

## Fixed engine, customizable policy

The fixed engine owns:

- artifact identity, versions, and lineage;
- approval tied to an exact version;
- policy snapshots and digests;
- evidence provenance and trust stamping;
- shared verdict meanings;
- stale-handoff detection;
- auditable human and agent decisions.

Teams customize:

- required document roles and topics;
- which gates run;
- which gates are required or advisory;
- where policies apply;
- accepted evidence;
- executor choice;
- team-specific Skills used as evaluation rubrics.

## The three governance levels

| Level | Typical use | Default posture |
|---|---|---|
| `light` | small, low-risk changes | minimum contract, focused evidence |
| `standard` | ordinary engineering work | core readiness, AC evidence, human approval |
| `enhanced` | high-impact or protected work | expanded readiness, stronger evidence, independent review |

These levels are understandable defaults, not rigid process templates.

## How a level is resolved

When publishing, the author declares impact signals such as:

- work type and impact level;
- rollback complexity;
- blast radius;
- protected-domain involvement;
- preferred governance level.

SpecGate resolves those signals deterministically. Policy may raise a requested
level when risk requires stronger governance. It does not silently weaken it.

The result and reason codes are stored with the artifact.

Inspect a work item:

```bash
specgate work policy "$WORK_REF"
```

## Readiness before implementation

Readiness asks:

> Is this artifact complete and clear enough for review or implementation?

Checks may cover:

- goal, scope, and non-goals;
- observable acceptance criteria;
- constraints and risks;
- rollback and rollout;
- verification approach;
- required document roles;
- team-specific semantic questions.

Readiness may include deterministic checks, platform-model judgments, IDE-agent
tasks, or human decisions.

A readiness pass means the checked contract is ready for human review or the
next governed step. It is not human approval.

## Delivery review after implementation

Delivery review asks:

> Does reported implementation evidence credibly satisfy each acceptance criterion?

It uses:

- coding-agent completion claims;
- checks run by the coding or verifier agent;
- evidence manifests;
- PR/MR and CI webhook signals;
- the evidence policy frozen with the artifact;
- optional team Skills.

A tracker item marked `Done` is a workflow signal, not a SpecGate `Pass`.

## Gate evaluation modes

| Mode | Purpose |
|---|---|
| Structural | Evaluate fields, roles, topics, or other deterministic records |
| Semantic | Judge bounded artifact meaning using a rubric |
| Evidence policy | Evaluate evidence coverage, trust, freshness, or independence |
| Human decision | Require an explicit human judgment |
| External assertion | Accept a result from a defined external producer |

Custom structural and evidence-policy gates use a closed declarative rule
language. Arbitrary server-side plugin code is not the extension model.

## Who can execute a gate

| Executor | Use |
|---|---|
| `deterministic` | SpecGate evaluates structured records |
| `ide_agent` | A coding or verifier agent evaluates within the IDE |
| `platform_llm` | SpecGate’s configured model evaluates bounded content |
| `human` | A person records the decision |
| `external` | An external tool submits an assertion |

Executor choice is part of policy. A team may prefer IDE agents for semantic
checks that benefit from repository context. SpecGate itself does not need
general Git read access.

## Verdicts and evidence

Common verdicts:

| Verdict | Meaning |
|---|---|
| `pass` | Requirement satisfied with sufficient evidence |
| `warn` | Concern exists but policy treats it as advisory |
| `fail` | Required condition is not satisfied |
| `needs_human_review` | Automation cannot safely decide |
| `not_applicable` | Gate genuinely does not apply |
| `not_run` | No valid result exists yet |

Every meaningful verdict should include:

- evaluator and executor;
- gate and artifact digest;
- summary and findings;
- evidence references;
- timestamp and trust information.

Gate effect depends on resolved policy and route. Blanket language that all
quality gates are non-blocking is incorrect.

## Skills as team rubrics

A Skill is reusable Markdown guidance for an evaluator. A policy can bind a
Skill to a semantic gate or delivery review.

Examples:

- API compatibility review;
- payment rollback expectations;
- accessibility review;
- migration safety;
- domain-specific acceptance-criteria quality.

Skills customize the rubric. They do not bypass the shared result schema,
versioning, provenance, or policy rules.

## Human approval remains distinct

Automation can prepare evidence and recommend verdicts. Human approval remains
a separate lifecycle action when policy requires it.

Current deployments use cooperative identity for much of the product.
Do not interpret a recorded actor label as compliance-grade identity assurance.

## Continue

- [Trust and security](trust-and-security.md)
