# Artifacts and Context Packs

Artifacts are versioned document bundles. Context Packs are the governed
implementation briefs created from approved work and artifacts.

## Artifact packages are flexible

SpecGate does not require one spec format. An artifact package can contain a
PRD, design, implementation plan, verification notes, research, or framework
specific documents.

Each document has:

- `path`: its identity inside the package, such as `spec.md`;
- `role`: its governance purpose, such as `spec`, `design`, `plan`,
  `verification`, `research`, or `custom:*`;
- content supplied by the CLI, API, or authoring tool.

The role matters more than the filename. A file named `notes.md` can still be
the authoritative spec if it is published with role `spec`.

## The governance envelope is fixed

Documents are flexible, but the artifact envelope is stable. It records:

- feature linkage;
- source kind and source revision;
- author or service metadata;
- impact and requested governance level;
- gates profile snapshot;
- artifact status;
- version and lineage.

This envelope lets SpecGate compare artifacts from different authoring tools.

## Versions are immutable

Every publication creates an artifact version. Approved artifacts are not edited
in place. Changes create a new draft or proposal, then another reviewed version.

This protects the handoff: a coding agent can prove it used the exact version a
human approved.

## Canonical artifacts

A feature can have several artifacts. The canonical artifact is the current
source of truth for that feature. Making an artifact canonical is a reviewed
action; draft or incomplete content should not become canonical.

## Context Packs

A Context Pack is the implementation contract for a coding agent. It can include:

- work title and description;
- acceptance criteria;
- approved artifact summary and file references;
- scope limits and blast radius;
- risks and assumptions;
- applicable skills;
- unresolved gates or review feedback;
- route-specific notes.

Agents should implement from the Context Pack, not from memory or chat history.

## Quick route and full route

The quick route creates a lightweight work item and Context Pack from title,
description, and acceptance criteria. It is useful for small changes and
bugfixes.

The full route starts from an artifact package. It is useful when work needs an
approved spec, design, plan, or verification document before implementation.

Both routes produce delivery evidence and review.

## Where artifact data lives

In the default local stack:

| Storage | Contents |
|---|---|
| Postgres | artifact metadata, file index, status, lineage, gates, events |
| `doc-registry-data` volume | artifact/spec document contents under `/data/blobs` |

Purging local data deletes both metadata and document contents. Back up both
stores before running destructive cleanup.

## Proposing updates

When implementation reveals a spec gap, do not mutate the approved artifact
directly. Open a governed proposal:

```bash
specgate artifact propose <artifact-id> --file proposal.json
```

The proposal can be reviewed and materialized as a new artifact version.

## Related

- [How SpecGate works](how-specgate-works.md)
- [Governance and gates](governance-and-gates.md)
- [CLI workflow](../guides/cli-workflow.md)
