# Artifacts and Context Packs

An artifact is a versioned snapshot of your source documents. A Context Pack is
the focused implementation brief a coding agent receives after that work is
ready.

## Keep your existing document layout

SpecGate does not require one spec format or directory structure. A package can
contain a PRD, design, implementation plan, verification notes, research, or
framework-specific documents.

Each document has:

- `path`: its identity inside the package, such as `spec.md`;
- `role`: its governance purpose, such as `spec`, `design`, `plan`,
  `verification`, `research`, `reference`, or `custom:*`;
- content supplied by the CLI, API, or authoring tool.

When a local package is selected, `path` is its normalized repository-relative
POSIX path. SpecGate preserves nested paths; it does not flatten framework
directories. Local and Full modes reject absolute, traversal, backslash, and
NUL-containing paths with the same rules. Roles are lowercased; unknown roles
become `unspecified`. The server snapshots bytes and computes per-file SHA-256
plus a deterministic package manifest digest.

The role matters more than the filename. A file named `notes.md` can still be
the authoritative spec if it is published with role `spec`.

## SpecGate adds a consistent record around the documents

Documents are flexible, but the artifact envelope is stable. It records:

- feature linkage;
- source kind and source revision;
- author or service metadata;
- impact and requested governance level;
- policy snapshot;
- artifact status;
- version and lineage.

This envelope lets SpecGate compare artifacts from different authoring tools.
Any spec-driven layout uses the same framework-neutral package envelope.
Callers explicitly assign paths and roles; SpecGate storage does not detect a
framework or infer behavior from directory names.

## Versions are immutable

Every publication creates an artifact version. Approved artifacts are not edited
in place. Changes are published as a new draft version for review.

Existing `source_kind`, `source_id`, `source_revision`, and `created_by` fields
record provenance. Stored document hashes identify exact captured bytes even
when a Git working tree is dirty. A local file is not durable in SpecGate until
publication succeeds.

For updates, an IDE agent can compare an explicitly mapped local package with
one selected artifact version. The CLI reads stored file hashes, not old file
content, and reports added, removed, changed, or unchanged paths. Comparison is
preparation evidence only: it does not publish, approve,
promote, link work, or rebuild a Context Pack.

This protects the handoff: the team can show that the coding agent received the
exact version a human approved.

## Canonical artifacts

A feature can have several artifacts. The canonical artifact is the current
source of truth for that feature. Making an artifact canonical is a reviewed
action; draft or incomplete content should not become canonical.

## Context Packs

A Context Pack is the agent's implementation brief. It can include:

- work title and description;
- acceptance criteria;
- approved artifact summary and file references;
- scope limits and blast radius;
- risks and assumptions;
- applicable skills;
- unresolved gates or review feedback;
- route-specific notes.

The agent should start from the Context Pack instead of reconstructing the task
from memory or chat history.

## Quick route and full route

The quick route in either mode creates a lightweight work item from title, description, and
acceptance criteria. `work context` derives its Context Pack from that persisted
work item; it is not a separately stored artifact. It is useful for small
changes and bugfixes.

The Local-or-Full route starts from an artifact package. It is useful when work needs an
approved spec, design, plan, or verification document before implementation.
After human approval, the artifact is promoted, feature-backed work is created
against that exact canonical artifact, and `work context` assembles the Context
Pack. Each step is a separate durable state and can be resumed safely.

Both routes produce delivery evidence and review.

## Local data

The Full appliance keeps artifact metadata and immutable document contents in
its managed `specgate-data` volume. Purging local data deletes both; back up
the appliance before destructive cleanup.

## Publishing updates

When implementation reveals a spec gap, do not mutate the approved artifact
directly. Have the IDE agent publish a new version:

```bash
specgate artifact publish --file artifact.json
```

The new version can be reviewed through the normal artifact-decision flow.

## Related

- [How SpecGate works](how-specgate-works.md)
- [Governance and gates](governance-and-gates.md)
- [CLI workflow](../guides/cli-workflow.md)
