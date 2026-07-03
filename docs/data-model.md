# Data model

This reference describes the main SpecGate domain objects and where their data
is stored. It is a product-level model, not a database schema.

## Storage locations

Default local deployments use two persistent Docker volumes:

| Store | Data |
|---|---|
| Postgres (`postgres-data`) | features, work items, artifacts, artifact file index, settings, evidence, gates, events |
| Doc Registry blob root (`doc-registry-data`) | artifact/spec document contents under `/data/blobs` |

Purging both volumes deletes artifact/spec metadata and document contents.

## Feature

A feature is a product capability. It can have:

- a stable key;
- display metadata;
- a canonical artifact;
- linked work items;
- optional summary or taxonomy data.

## Change request / work item

A change request is one governed unit of delivery. It stores:

- title and description;
- work type and route;
- acceptance criteria;
- selected workspace and creator attribution;
- links to a feature and lead artifact when available;
- Context Pack identifiers and delivery state.

Board phase is derived from work and artifact state. The attention queue is a
read model, not a separate workflow object.

## Artifact

An artifact is an immutable versioned package. It stores:

- feature linkage;
- version and lineage;
- status;
- governance profile snapshot;
- impact and source metadata;
- document index.

The document contents are stored in blob storage. The artifact rows and file
metadata are stored in Postgres.

## Artifact edit session

An edit session is a draft-only workspace for proposed artifact updates. It is
anchored to a base artifact and version so SpecGate can detect stale edits.

Approving or materializing a draft creates a new artifact version. It does not
mutate the approved artifact in place.

## Gate run

A gate run records one governance evaluation. It stores:

- subject kind, such as artifact or change request;
- gate key and version;
- executor;
- verdict state;
- evidence payload;
- timestamps.

Gate runs support readiness checks, delivery review, and audit history.

## Context Pack

A Context Pack is an implementation contract assembled for a coding agent. It is
derived from work item state, accepted criteria, policy, artifact references,
gate state, and skills.

It is the handoff record: what the agent was allowed to implement from.

## Delivery evidence

Delivery evidence is recorded as feedback events and gate results. It can come
from:

- coding-agent completion reports;
- manual notes;
- CI or webhook integrations;
- delivery-review verdicts.

Evidence includes provenance so review can distinguish self-reported and
corroborated signals.

## User and workspace

Users and workspaces are local product objects for attribution and filtering.
They are not authentication sessions in the alpha stack.

The CLI stores the selected user and workspace in local config. New quick work
items use that selection by default.

Quick work items store `created_by` and `workspace_id` for attribution and selection scoping.

## Integration

An integration stores provider connection data and linked external resources,
such as repositories, projects, teams, webhooks, or tracker items.

Integration events can become corroborated delivery evidence when they can be
matched to a work item.

## Skill

A skill is a reusable instruction or rubric. Skills can appear in Context Packs
and gate configuration. IDE plugin installation writes focused SpecGate skills
to supported IDEs.

## Settings

Settings store server-side model choices, provider keys, governance thresholds,
and feature flags. Sensitive values are encrypted with
`SETTINGS_ENCRYPTION_KEY`.

Back up the encryption key with the database. Losing it makes encrypted settings
unrecoverable.

## Event

Events are append-only records of important workflow changes, such as artifact
publication, status changes, feedback, or feature canonical changes.

## Relationships

```text
Workspace
  └─ Change request
       ├─ Acceptance criteria
       ├─ Context Pack
       ├─ Delivery evidence
       └─ Gate runs

Feature
  ├─ Canonical artifact
  ├─ Artifact versions
  └─ Change requests

Artifact
  ├─ Artifact files
  ├─ Gate runs
  ├─ Events
  └─ Edit sessions / proposals
```

## Related

- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md)
- [Evidence reference](reference/evidence.md)
- [Contracts](contracts.md)
