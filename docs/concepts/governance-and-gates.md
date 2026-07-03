# Governance and gates

Governance is how SpecGate decides what must be true before a coding agent
receives context and before delivery is accepted.

## Policy decides the level of control

SpecGate resolves a governance level from the work and artifact metadata. The
level can depend on impact, requested governance level, source, feature state,
or team policy.

The built-in levels are:

| Level | Meaning |
|---|---|
| `light` | minimum contract and focused evidence |
| `standard` | normal readiness, acceptance-criteria evidence, and human approval |
| `enhanced` | expanded readiness, stronger evidence, and independent review |

See [Governance reference](../reference/governance.md) for exact resolution
rules.

## Readiness checks happen before implementation

Readiness gates ask whether an artifact is ready to hand off. Examples:

- required document roles are present;
- acceptance criteria are testable;
- scope and blast radius are clear;
- risks or dependencies are named;
- required skills are attached.

Readiness does not approve the artifact. It informs review.

## Delivery review happens after implementation

Delivery review checks whether evidence supports the acceptance criteria and
policy obligations. It can use:

- completion reports;
- tests and build output;
- files changed;
- gate runs;
- CI or integration events;
- human or agent notes.

If evidence is missing or weak, review can fail or require human review.

## Gate verdicts

Common verdict states:

| State | Meaning |
|---|---|
| `pass` | requirement satisfied |
| `warn` | issue exists, but may not block |
| `fail` | requirement not satisfied |
| `needs_human_review` | model or evidence cannot decide safely |
| `not_run` | no current result |
| `not_applicable` | gate does not apply |

## Evidence quality matters

Self-reported agent evidence is useful, but corroborated evidence is stronger.
Examples of corroborated evidence include CI events, merged PR events, or
trusted webhooks. An enhanced policy may require corroborated evidence before a
delivery review can pass.

## Skills act as team rubrics

Skills are reusable team instructions. A governance profile can attach skills
to gates, and Context Packs can point agents to the relevant skill prompts.
This lets teams keep local standards close to the governed workflow.

## Human approval remains separate

Passing gates does not replace human approval. Approval means a responsible
person accepted a specific artifact version or delivery state. Gates produce
evidence for that decision.

## Related

- [Governance reference](../reference/governance.md)
- [Evidence reference](../reference/evidence.md)
- [Artifacts and Context Packs](artifacts-and-context-packs.md)
