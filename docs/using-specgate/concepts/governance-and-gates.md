# Governance and gates

Governance answers two practical questions: is this work ready for a coding
agent, and does the returned evidence support accepting the result?

## Policy for Local work

Quick work begins with explicit criteria and skips artifact readiness.
Artifact-backed work uses fixed Standard governance. Each artifact freezes
required roles, topics, gates, rubrics, and human approval policy when
published.

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
- user-cited test and CI output;
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
| `needs_human_review` | unresolved scope or evidence needs human judgment |
| `not_run` | no current result |
| `not_applicable` | gate does not apply |

## Evidence quality matters

The review identifies agent claims, file citations, bound checks, and
CLI-observed results. Review CI output in its original context to see which
revision and test environment it covers. See [How verification works](verification.md).

## Skills act as team rubrics

Local project rubrics add team instructions to the four existing semantic
gates. Publication freezes them with the artifact. See
[Customize governance](../guides/customize-governance.md).

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
