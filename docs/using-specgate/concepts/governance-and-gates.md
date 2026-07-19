# Governance and gates

Governance answers two practical questions: is this work ready for a coding
agent, and does the returned evidence support accepting the result?

## Policy matches the checks to the risk

SpecGate resolves a governance level from explicit structured inputs: request
type, impact level, requested governance level, and the impact declaration
(protected domains, data/schema change, external contracts, rollback
complexity, and blast radius). It does not infer risk from titles,
descriptions, filenames, or keywords.

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

Readiness results help a person review the artifact; they do not approve it on
their own.

## Delivery review happens after implementation

Delivery review checks whether evidence supports the acceptance criteria and
policy obligations. It can use:

- completion reports;
- tests and build output;
- files changed;
- gate runs;
- user-cited or externally supplied test and CI output;
- repository PR/MR observations and optional Linear state;
- human or agent notes.

If the evidence is missing or unclear, SpecGate points out the gap or asks for a
human decision.

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

An agent's own report is useful, but independent evidence is stronger. A merged
PR/MR provides repository observation only when its submitted `head_sha` matches
the latest completion receipt head. An enhanced policy may require that
observation or deterministic bindings for every criterion before a delivery
review can pass; CI is not a first-release assurance source.

## Skills act as team rubrics

Skills are reusable team instructions. The resolved automatic policy can attach skills
to gates, and Context Packs can point agents to the relevant skill prompts.
This lets teams keep local standards close to the governed workflow.

## Human approval remains separate

Passing gates does not replace human approval. Approval means a responsible
person accepted a specific artifact version or delivery state. Gates produce
evidence for that decision.

## Related

- [How verification works](verification.md) - what context the checkers
  receive and how verdicts are derived
- [Governance reference](../reference/governance.md)
- [Evidence reference](../reference/evidence.md)
- [Artifacts and Context Packs](artifacts-and-context-packs.md)
