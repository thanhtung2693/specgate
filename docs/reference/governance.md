# Governance reference

This page summarizes SpecGate’s governance vocabulary. The
[cross-module contracts](../contracts.md) and
[Doc Registry specification](../../app/doc-registry/docs/spec.md) remain
canonical for wire behavior.

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

Resolution order:

```text
Built-in level base
→ deployment bindings
→ organization bindings
→ repository bindings
→ accepted work-item exceptions
```

Merge rules favor stronger requirements:

- `light < standard < enhanced`;
- `auto < self_approve < human_required`;
- `advisory < when_applicable < always`;
- `unit < integration < system`.

Policy resolution produces an immutable profile snapshot and digest stored with
the artifact version.

## Package layout

```text
package-root/
├── package.yaml
├── gates/
│   └── <gate>.yaml
├── policies/
│   └── <policy>.yaml
└── bindings/
    └── <binding>.yaml
```

Packages may also include reusable Skill or schema content where supported by
the definition contract.

## Gate identity

A gate uses a stable namespaced reference:

```text
<namespace>/<name>@<version>
```

Example:

```text
acme/api-compatibility@v2
```

Gate version and digest are frozen into tasks and results. A result with a stale
digest is rejected.

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

The built-in LLM readiness gates resolve their executor from deployment state: a
gate runs on `platform_llm` when a server-side model is configured, and is
dispatched to `ide_agent` (the developer's coding agent) when none is. Either way
the verdict feeds the same readiness aggregation; `ide_agent` results are stamped
`agent_attested` and `platform_llm` results `platform_evaluated`.

## Gate requirements

| Requirement | Effect |
|---|---|
| `always` | run and enforce |
| `when_applicable` | enforce when selector applies |
| `advisory` | may report concerns but never blocks |

Unknown applicability defaults to a safe unresolved outcome unless the gate
definition explicitly states otherwise.

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
- profile and input digest;
- phase;
- state and summary;
- findings and evidence references;
- confidence;
- evaluator mode, executor, name, version, run ID, and role;
- start and finish times;
- failure category when execution did not produce a valid judgment.

Timeout, unavailable model, corrupt input, or internal error produces
`not_run`, not a semantic `fail`.

## Policy bindings

Bindings activate an exact policy version for a stable scope:

- `deployment`;
- `organization`;
- `repository`.

Import stores bindings inactive. Activation affects future resolutions and does
not mutate existing artifact snapshots.

## Skills

Skills have explicit consumers:

- evaluation rubric;
- agent instruction.

Skill content and digest are frozen into the resolved profile. A pointer to the
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

## Corpus and outcomes

Benchmark corpus entries pair a gate version and input snapshot with an expected
verdict. They help evaluate gate changes.

Outcome feedback records downstream signals such as:

- human overrides;
- rejected evidence;
- post-merge rollbacks;
- escaped defects.

Policy health aggregates these signals for calibration. It does not
automatically rewrite policy.

## Conformance fixtures

Maintainers can use:

- [policy resolution cases](../conformance/governance-policy-v1/resolution-cases.json);
- [gate definition cases](../conformance/governance-policy-v1/gate-definition-cases.json);
- [policy merge cases](../conformance/governance-policy-v1/policy-merge-cases.json);
- [gate result cases](../conformance/governance-policy-v1/gate-result-validation-cases.json).

## Related

- [Governance and gates](../concepts/governance-and-gates.md)
- [Evidence reference](evidence.md)
