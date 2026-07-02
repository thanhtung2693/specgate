# Artifacts and Context Packs

Artifacts preserve intent. Context Packs give coding agents the approved slice
needed for one delivery.

## Artifact packages are flexible

An artifact is a versioned document bundle. Its files may follow any useful
structure:

- PRD, spec, frontend/backend/QA plans;
- OpenSpec proposals, designs, specs, and tasks;
- Spec Kit plans, research, data models, and tasks;
- architecture decisions;
- a single Markdown change description;
- a team-specific format.

SpecGate does not infer authority from a filename. Publishers declare document
roles, and readiness checks look for required topics wherever those topics live.

## The governance envelope is fixed

Documents are flexible, but every artifact follows the same governance
principles:

- stable artifact identity;
- immutable versions;
- source and source revision;
- declared document roles;
- policy snapshot and digest;
- status and approval history;
- provenance for gate verdicts and evidence;
- lineage between revisions.

Teams can customize what must be checked. They cannot redefine what versioned
approval, evidence provenance, or verdicts mean.

## Versions, source revisions, and lineage

Every publication creates a new immutable artifact version. A revision records:

- which prior version it follows;
- which authoring tool or source produced it;
- the source revision when available;
- which governance policy was resolved at publication time.

Policy changes later do not rewrite history. An old artifact remains explainable
using the policy snapshot stored with it.

## Approval belongs to one version

Approval never means “this feature in general looks fine.” It means a specific
artifact version was reviewed under a specific policy.

If content changes after approval, the new version needs its own readiness and
review. The old approval is not copied forward.

## Canonical artifacts

A Feature may have several linked artifacts. Its canonical artifact is the
current source of truth for product intent.

Making an artifact canonical is a reviewed action. Draft or incomplete content
must not silently replace the approved source of truth.

## Context Packs

A Context Pack packages the implementation contract for one work item. It may
contain:

- approved intent and scope;
- acceptance criteria;
- required artifact documents;
- domain vocabulary from glossary or language-reference files;
- design references and governed knowledge;
- governance level and gate results;
- risks, constraints, rollout, or verification requirements;
- unresolved warnings the coding agent must see.

The coding agent reads the Context Pack before editing code:

```bash
specgate work context "$WORK_REF"
```

If more detail is needed:

```bash
specgate artifact files "$ARTIFACT_ID" spec.md tasks_fe.md
```

This returns file references by default; add `--content` only when the file body
is required.

The approved Context Pack outranks chat history, tracker comments, and stale
repository copies.

## Reference attachments

Attachments are governed supplemental references for a Feature: screenshots,
logs, mockups, external docs, customer examples, or repro evidence that should
help a future gate, review, or coding-agent handoff. They are not a replacement
for artifact documents.

Adding a random image or document to an IDE-agent chat does **not**
automatically create a SpecGate attachment. That file stays local/session
context unless a user or agent explicitly pins it to SpecGate with an audience:

- `gate` — used by quality/readiness gates;
- `coding_agent` — rendered into future Context Packs;
- `both` — used by gates and future coding-agent handoffs.

If the material changes intent, scope, acceptance criteria, rollout, or design
contract, it should become an artifact document through a reviewed proposal
instead of a loose attachment.

## Quick route and full route

### Quick route

Quick work uses a narrow Context Pack focused on the issue, intended change,
acceptance criteria, and constraints. It is useful for low-risk bugs and small,
well-understood changes.

### Full route

Full work uses the approved artifact bundle and may require richer roles and
topics: product intent, technical contract, implementation plan, QA, rollout,
risks, and design references.

Quick does not mean ungoverned. Full does not mean every team must use identical
files.

## Domain vocabulary

Context Packs include a derived `Domain Vocabulary` section when the approved
artifact carries vocabulary material. Teams can provide this as files named
`glossary.md`, `vocabulary.md`, `domain-vocabulary.md`, `domain-language.md`,
`ubiquitous-language.md`, or `CONTEXT.md`, or as custom roles such as
`custom:glossary` or `custom:domain-language`.

Use this for terms whose meaning affects implementation: status names, policy
names, workflow phrases, product nouns, or domain-specific verbs. The section is
assembled from existing artifact files; it does not require separate storage or
change the Context Pack schema.

## Stale handoffs

Context Packs are tied to their source versions.

```text
Artifact v7 approved
→ Context Pack generated from v7
→ artifact changes to v8
→ old Context Pack becomes stale
→ new review and handoff required
```

This prevents a coding agent from implementing an older approved idea after the
source of truth changes.

## Proposing a governed update

A coding agent may discover ambiguity or a necessary contract change. It must
not mutate approved content directly.

Instead:

```bash
specgate artifact propose "$ARTIFACT_ID" --file proposal.json
```

The proposal enters human review. Once accepted, SpecGate materializes a new
artifact version with preserved lineage.

## Continue

- [Governance and gates](governance-and-gates.md)
- [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md)
- [Glossary](../reference/glossary.md)
