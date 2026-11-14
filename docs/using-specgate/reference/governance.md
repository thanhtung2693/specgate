# Governance

Use this reference to look up governance vocabulary, levels, policy resolution,
and gate results.

For command behavior, use the [CLI reference](cli.md). For available gate
definitions, use the [gate catalog](gates.md).

## Governance levels

Artifact-backed Local work uses Standard. Its required roles are `spec`
and `plan`, with outcomes, scope, acceptance criteria and verification method.
Design and verification material can use their optional roles.

## Policy resolution

Every artifact version stores an immutable policy snapshot and digest. The
snapshot names the exact gates, approval policy, and evidence policy that apply
to that version. Gate dispatch and artifact approval fail closed when the
snapshot is missing, changed, or unsupported.

Local embeds the
default rubrics and optionally appends project-owned Markdown instructions from
the four fixed `.specgate/rubrics/<gate-key>.md` files at publication. The
effective text and relative source are frozen into that version; absent files
preserve the exact default policy and digest. Local has no UI rubric editor.
See [Customize governance](../guides/customize-governance.md#add-local-project-instructions)
for names, validation and Git ownership.

## Gate identity

Built-in readiness gates use stable keys such as `scope_clear` and
`acceptance_criteria_verifiable`. When a gate is dispatched to an IDE agent,
its task carries a frozen gate digest. Results with a stale digest are rejected.

## Evaluation modes

| Mode | Evaluation |
|---|---|
| `structural` | deterministic platform records |
| `semantic` | bounded content with a versioned rubric |
| `evidence_policy` | evidence and recorded provenance |
| `human_decision` | explicit human outcome |

## Executors

| Executor | Location |
|---|---|
| `deterministic` | SpecGate |
| `ide_agent` | coding or verifier IDE |
| `human` | governed human action |

Evaluation mode describes what a gate means. Executor describes where a
non-deterministic evaluation runs.

Local semantic tasks bind the artifact
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
- findings and evidence references;
- evaluator mode, executor, name, version, run ID, and role;
- start and finish times;
- failure category when execution did not produce a valid judgment.

Pending Local task results are reported as `not_run`.

## Skills

Skills have explicit consumers:

- evaluation rubric;
- agent instruction.

The rubric text and digest are frozen into the artifact policy.

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

## Local acceptance basis

For enhanced Local work, `change status` returns a `basis_digest` with the
exact `review_id`. A human decision repeats both values, and any selected
checkpoint or impact pair, so it is attached to the inspected evidence rather
than a later replacement. The stored basis preserves the human actor, note,
and observed gaps; it is not a confidence score, identity proof, or an approval
of a different artifact version. Legacy Local work with no enhanced record and
no optional comparison keeps the review-ID-only flow.

## Related

- [Gate catalog](gates.md)
- [Governance and gates](../concepts/governance-and-gates.md)
- [How verification works](../concepts/verification.md)
- [Evidence reference](evidence.md)
