# Governance

Use this reference to look up governance vocabulary, levels, policy resolution,
and gate results.

For command behavior, use the [CLI reference](cli.md). For available gate
definitions, use the [gate catalog](gates.md).

## Governance levels

| Level | Control strength |
|---|---|
| `light` | minimum contract and focused evidence |
| `standard` | normal readiness, AC evidence, and human approval |
| `enhanced` | expanded readiness, stronger evidence, and independent review |

Resolution inputs include work type, impact level, requested level, rollback
complexity, blast radius, data/schema impact, external contracts, affected
systems, and protected domains.

Unknown protected-domain status fails safe to enhanced governance.

## Policy resolution

SpecGate recommends a built-in level from the request type, impact declaration,
protected-domain status, and requested governance level. A requested level can
raise that recommendation, but cannot lower it.

Every artifact version stores an immutable policy snapshot and digest. The
snapshot names the exact gates, approval policy, and evidence policy that apply
to that version. Gate dispatch and artifact approval fail closed when the
snapshot is missing, changed, or unsupported. Local mode uses
`human_required` approval with `attested_ok` evidence.

## Gate identity

Built-in readiness gates use stable keys such as `scope_clear` and
`acceptance_criteria_verifiable`. When a gate is dispatched to an IDE agent,
its task carries a frozen gate digest. Results with a stale digest are rejected.

## Evaluation modes

| Mode | Evaluation |
|---|---|
| `structural` | deterministic platform records |
| `semantic` | bounded content with a versioned rubric |
| `evidence_policy` | evidence and server-stamped provenance |
| `human_decision` | explicit human outcome |
| `external_assertion` | signed or server-observed external result |

## Executors

| Executor | Location |
|---|---|
| `deterministic` | SpecGate |
| `ide_agent` | coding or verifier IDE |
| `platform_llm` | configured SpecGate model |
| `human` | governed human action |
| `external` | external producer |

Evaluation mode describes what a gate means. Executor describes where a
non-deterministic evaluation runs.

The built-in model-judged readiness gates resolve their executor from
deployment state: a gate runs on `platform_llm` when a server-side model is
configured, and is dispatched to `ide_agent` (the developer's coding agent)
when none is. Either way the verdict feeds the same readiness aggregation;
`ide_agent` results are stamped `agent_attested` and `platform_llm` results
`platform_evaluated`.

Local IDE gate tasks and Full model-less IDE gate tasks bind the artifact
snapshot, gate digest, frozen rubric, workspace, executor, and expiry. A
successful `gates check` remains `not_run` until every required IDE result is
submitted; that pending state is not readiness approval.

## Verdict states

- `pass`
- `warn`
- `fail`
- `needs_human_review`
- `not_applicable`
- `not_run`

Required `fail`, `needs_human_review`, or `not_run` prevents an aggregate pass.
Required `warn` may allow progress while remaining visible. Advisory results do
not block.

## Common result content

A GateResult records:

- gate key, version, and digest;
- policy and input digest;
- phase;
- state and summary;
- findings and evidence references (confidence, when an evaluator reports one,
  rides inside `findings` — it is not a top-level field the submit endpoint
  accepts);
- evaluator mode, executor, name, version, run ID, and role;
- start and finish times;
- failure category when execution did not produce a valid judgment.

Timeout, unavailable model, corrupt input, or internal error produces
`not_run`, not a semantic `fail`.

## Skills

Skills have explicit consumers:

- evaluation rubric;
- agent instruction.

Skill content and digest are frozen into the resolved policy. A pointer to the
current registry Skill is navigation metadata, not runtime authority.

## Acceptance-criteria minimum

Delivery-relevant ACs retain:

- stable ID;
- content digest;
- observable statement;
- verification method;
- individual delivery claim and verdict;
- evidence links when claimed satisfied.

Changing AC text or verification changes its digest. Older evidence becomes
stale for the new content.

## Related

- [Gate catalog](gates.md)
- [Governance and gates](../concepts/governance-and-gates.md)
- [How verification works](../concepts/verification.md)
- [Evidence reference](evidence.md)
