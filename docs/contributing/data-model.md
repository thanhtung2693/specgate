# Data Model

Use this reference when changing persistence, entity relationships, or data
ownership. It describes the product-level model rather than every table; the
authoritative development schema is
[app/doc-registry/migrations/postgres/0001_init.migration](../../app/doc-registry/migrations/postgres/0001_init.migration).

## Storage

| Store | Data |
| --- | --- |
| Postgres | metadata, workboard, gates, evidence, settings, Knowledge chunks, workspace-scoped integrations |
| Local blob root or S3/MinIO | artifact files, Knowledge source files, governance uploads |
| Redis | optional async webhook / Knowledge ingest queue |

New product object keys are required to use the
`workspaces/{workspace_id}/` prefix. Redis task payloads carry the
parent-derived workspace, and workers re-read the parent under that scope. Repository metadata, content,
and context-pack caches include workspace in their keys. pgvector payloads require
`workspace_id`; searches always apply the exact workspace filter.

Postgres is required. Blob storage defaults to the local `doc-registry-data`
volume in local installs and can be switched to S3/MinIO. Redis is optional.

## Core Entities

### User and Workspace

Users and workspaces provide attribution, team visibility, and selection
filters. They are not authentication or authorization records in the alpha
stack.

Workspace membership answers: "who is working in this space?" It does not grant
or deny access.

### Feature

A Feature is a durable product capability. It has a stable key, optional
display metadata, an optional canonical artifact, linked work items, and an
optional human-maintained summary.

Features are workspace-owned governed roots. The key is unique within one
workspace, so two workspaces may use the same human-facing key without sharing
records.

### ChangeRequest / Work Item

A ChangeRequest is one governed unit of delivery. It stores:

- title, description, work type, and route;
- workspace and creator attribution;
- feature link;
- acceptance criteria;
- lead artifact and Context Pack links;
- archive metadata.

Every normal WorkBoard read or mutation is scoped by a required workspace
context. Missing context fails closed; cross-workspace detail or mutation is
returned as not found. Internal callback paths are separate from product
reads and must derive ownership from their stored parent.

The board phase is derived on read from artifacts, Context Packs, gate runs,
delivery review, and archive state. It is not a separate persisted status enum.

### AcceptanceCriterion

Acceptance criteria are normalized rows attached to a ChangeRequest. Each row
has a stable ID used by completion and review evidence, plus the criterion text,
sort order, source, and optional human-authored `verification_binding` such as
`tests`.

The ChangeRequest also carries a synchronized JSON display mirror. It cannot
preserve criterion identity and is never a fallback for the normalized rows.

### Artifact

An Artifact is an immutable versioned package. It stores:

- owning workspace id;
- feature id and version;
- lifecycle status;
- lineage;
- source and impact metadata;
- normalized document paths, server-computed content hashes, and snapshot digest;
- existing source kind/id/revision and publisher provenance;
- governance policy snapshot;
- document file index.

Document contents live in blob storage under artifact-ID-scoped keys. Approved
artifact snapshots are not edited in place.

Artifacts inherit the same workspace boundary as their linked Feature or owning
work item. Normal reads and mutations are scoped by workspace, and wrong-
workspace IDs are indistinguishable from unknown IDs.

### FeatureAttachment

Feature attachments are workspace-owned children for links, screenshots, and
governance-file references. `workspace_id` is stored on every row and must
match the Feature and referenced Governance File. Context Pack reads receive
the CR workspace explicitly; attachment IDs from another workspace are not
visible.

### GovernanceFile

`governance_files.workspace_id` owns internal upload metadata and its object
reference. Scoped reads and mutations filter by workspace. Development data is
rejected at write time without an owner. Object keys remain globally unique so
storage cleanup cannot address two rows ambiguously.

### GateRun

A GateRun records one governance evaluation over an artifact or ChangeRequest.
It stores the owning `workspace_id`, subject, gate key, state, executor, optional
hint, evidence JSON, and timestamps. Artifact and ChangeRequest
subjects derive the run workspace from their parent; unresolved ownership fails
the write.

### GateTask

A GateTask is a frozen IDE-agent evaluation request. It stores `workspace_id`,
artifact, gate/profile digests, executor, frozen Skill content, expiry, and
creation time. Task list, detail, dispatch, and result submission operations
require the selected workspace; task IDs are not global lookup authority.

Readiness checks, delivery review, human delivery decisions, and IDE-agent gate
task results all converge here.

### GovernanceFeedbackEvent

Feedback events are append-only delivery and review signals: coding-agent
completion, blocked ambiguity, peer review, user-cited or externally supplied
check output, PR/MR, Linear, and comment events. SpecGate does not ingest
provider CI state or create assurance from it. Each event has a required
workspace owner and never mutates approved artifacts directly. A reviewer can
resolve an event to `processed` or `ignored`.

### Context Pack

A Context Pack is the coding-agent handoff assembled from existing records. It
is derived on read; it is never stored on a ChangeRequest.

### Governance Knowledge

Governance Knowledge is workspace-scoped reference material uploaded by humans.
It has document lineages, immutable versions, chunks, optional embeddings, links
to features/work, and authority/type metadata.

Knowledge detail and mutation repository operations inherit the trusted API
workspace context; cross-workspace document IDs are treated as not found below
the handler boundary as well as at it.

Knowledge is untrusted reference context. Approved artifacts remain the source
of truth.

### Integration

Integrations store provider connection metadata, linked resources, webhook inbox
rows, delivery links, tracker links, OAuth state, provider tokens, and
resource-scoped managed webhook credentials. New integration roots carry
`workspace_id`; provider/name uniqueness is scoped to that workspace.
Integration root catalog and lifecycle calls fail closed without workspace
context; UI catalog and lifecycle calls include the selected workspace.

GitHub/GitLab delivery links retain a provider `head_sha` and separate
`merge_commit_sha`. Only a marked merged PR/MR whose `head_sha` matches the
latest completion's `git_receipt.head_revision` is `repository_observed`; the
merge SHA is inspection metadata. A ChangeRequest has at most one primary
Linear tracker link. It retains the selected team `resource_id`, makes handoff
idempotent, and permits a best-effort Done transition only after human
acceptance.

Integration events can become corroborated delivery evidence when correlated to
a work item. Resources, OAuth state, webhook inbox rows, delivery links, tracker
links, and integration-backed feedback derive ownership from their parent
integration; scoped reads and writes cannot cross that parent workspace. Empty
`integration_id` feedback is agent-originated internal data and stores the
trusted agent workspace directly; it is available only through that workspace.

### Skill

Skills are workspace-owned team rubrics or agent instructions. Names are unique
within a workspace; scoped REST/CLI reads and mutations require `workspace_id`.
Fresh installs require an owner for every row; development does not retain an
ownership-backfill path. Context
Packs include matching Skill IDs; IDE plugin installation remains CLI-managed.

### Governance policy

Automatic governance policy is code-defined and global. Each artifact version
stores its own immutable `policy_version`, `policy_digest`, and
`policy_snapshot_json`; later readiness, handoff, and delivery review consume
that frozen snapshot instead of any mutable catalog row.

### Settings

Settings store model configuration, embedding configuration,
retention behavior, and feature toggles. Sensitive values are encrypted with
`SETTINGS_ENCRYPTION_KEY`.

## Relationships

```text
Workspace
  -> workspace_members -> User
  -> ChangeRequest
     -> AcceptanceCriterion
     -> Context Pack
     -> GateRun
     -> GovernanceFeedbackEvent

Feature
  -> canonical Artifact
  -> Artifact versions
  -> ChangeRequests
  -> Knowledge links

Artifact
  -> Artifact files
  -> GateRun
  -> events

Integration
  -> resources
  -> webhook events
  -> delivery/tracker links
  -> feedback events
```

## Related

- [Architecture](architecture.md)
- [Contracts](contracts.md)
- [Evidence reference](../using-specgate/reference/evidence.md)
