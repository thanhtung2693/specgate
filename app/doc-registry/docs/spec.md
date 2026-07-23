# Doc Registry - Technical Specification

**Status:** current
**Document type:** reference
**Stack:** Go, Huma v2, Postgres + pgvector, local blob storage, optional S3/MinIO, optional Redis queue

This file is the canonical Doc Registry contract. Keep it short enough to review
in one pass. Historical cleanup notes belong in issues or implementation plans,
not in this contract.

## 1. Purpose and Boundaries

Doc Registry is SpecGate's durable store for governed work:

- artifact packages and immutable artifact files;
- workboard Features, ChangeRequests, acceptance criteria, gates, and delivery
  review readbacks;
- Governance Knowledge documents, chunks, and embeddings;
- lightweight governance-chat thread summaries;
- native source-control/tracker integrations and webhook inbox rows;
- IDE/CLI agent package assets.

Doc Registry stores, reads, validates, and records state. It does not own model
reasoning. Agents service calls perform model-judged readiness, quick-work
acceptance-criteria drafting, and delivery review when configured.

Out of scope:

- public internet exposure without an internal proxy boundary;
- JWT/RBAC enforcement at the Doc Registry HTTP layer;
- direct UI writes to object-store credentials;
- hidden fallback data in user-facing live surfaces.

## 2. Context Pack and Handoff Contract

Context Packs are derived on read. They are not separately mutable domain state.

### 2.1 Context Pack Payload

Context Pack reads return a JSON object:

| Field | Contract |
| --- | --- |
| `state` | `"assembled"` or `"not_generated"` |
| `markdown` | Rendered handoff document |
| `source_artifact_id` | Artifact used to assemble the pack, or empty |
| `knowledge_provenance` | CR-scoped only; always an array when present |
| `warnings` | Non-fatal assembly warnings |

`knowledge_provenance` rows are sorted by authority
(`source_of_truth`, `high`, `reference`, `low`) and then title; multiple
versions of one document dedupe to the latest version when known.

Quick-route bug-fix work without a source artifact still returns `assembled`:
its lightweight pack is derived from the persisted ChangeRequest intent and
acceptance criteria. It is not an artifact and cannot be edited or versioned
outside the IDE-authored artifact workflow.

`not_generated` is reserved for work with no usable source-artifact reference.
When a stored reference exists, an unavailable reader or failed artifact lookup
fails the request instead of hiding the problem as an empty Context Pack.
Assembly also fails when required artifact files, current gate state, delivery
review/completion state, artifact readiness state, or configured reference
attachments cannot be read. These are governance inputs, so omitting them would
produce an incomplete handoff that looks authoritative. Optional knowledge
enrichment remains non-fatal and is reported through `warnings`.
Non-empty source-artifact policy snapshots must satisfy the versioned snapshot
contract in section 8; Context Pack rendering does not interpret unversioned or
invalid JSON as a partial profile.

Rows in `acceptance_criteria` are the canonical criteria because their stable
IDs bind completion and review evidence. `acceptance_criteria_json` is only a
transactionally synchronized display mirror. Reads never recreate rows from the
mirror, and Context Pack assembly fails if canonical criteria are unavailable.

Unresolved gate verdicts (`warn`/`fail`/`needs_human_review`) ride the pack as an
"Unresolved Quality Gates" section so an execute-anyway handoff keeps the gaps.
`spec_repo_drift` is an artifact-scoped readiness run, not a CR gate_run, so the
renderer additionally pulls the source artifact's latest `spec_repo_drift`
readiness run and lists each finding (`doc_path` / `conflicting_claim` /
`spec_section`) as sub-bullets — otherwise the drifted-doc guidance would be
dropped from the full-route handoff.

The renderer groups artifact documents by role in this order: Spec, Design,
Implementation Plan, Verification, Research, Reference, Additional Documents.
Roles come only from published artifact metadata. Filenames and paths never
assign document meaning; custom or unspecified roles render under Additional
Documents.
Applicable Skill names come from the frozen policy snapshot. A current catalog
lookup may add descriptions and IDs, but an unavailable catalog cannot erase
the frozen names or change the gate rubric used for evaluation.
Rendered Markdown is capped at 96 KiB before returning it to a coding agent.
Oversized packs preserve the beginning and conclusion and include an explicit
truncation marker; the immutable artifact files remain unchanged and available
through artifact file reads.

### 2.2 Agent Loop

The handoff is CLI-first. Coding agents should read the Context Pack, implement
against approved spec content, report completion with `specgate delivery report`,
then trigger/read delivery review through the CLI.

Board phase derivation (which work items are ready for this loop) is defined in
§15 Artifact-Native Work Board.

## 3. Storage Model

Postgres is the only database backend and `POSTGRES_DSN` must be set. The
embedded Postgres migration file is the schema
authority; GORM `AutoMigrate` is not used. During development the schema is a
collapsed fresh-install `0001_init.migration`, replayed idempotently on every
start. It does not upgrade older databases. Startup checks the required columns
and stops with a clear incompatibility error when an older database is mounted;
reset that development database before starting this build.

Object storage is selected by `STORAGE_DRIVER`:

| Driver | Use |
| --- | --- |
| `local` | Local filesystem under `BLOB_DATA_ROOT`; default for local stacks |
| `s3` | S3/MinIO with configured endpoint, bucket, and credentials |

Local workspace-scoped blob IDs are validated before a filesystem path is
created; empty keys and literal `.` or `..` path segments are rejected rather
than normalized or stored.
The workspace ID is checked for path separators and parent-directory sequences
at the write boundary before it can become a local path component.
Local object storage also rejects symlinked key components and writes through a
unique temporary file before atomically replacing the final object.
When a request carries a workspace, local reads, metadata reads, and deletes
also require that ID's matching workspace prefix; unscoped maintenance cleanup
is the only exception.

Artifact publication requires a workspace. Files are stored under stable,
workspace-prefixed object keys:

```text
workspaces/{workspace_id}/{KeyPrefix}artifacts/{artifact_id}/{version}/{path}
```

Document `path` is caller-provided and validated: no empty path, absolute path,
`..`, or backslash.

### 3.1 Primary Tables

| Table | Purpose |
| --- | --- |
| `artifacts` | Artifact metadata, lifecycle state, version, lineage, policy snapshot |
| `artifact_files` | Immutable file metadata keyed by `(artifact_id, path)` |
| `artifact_services` | Impacted services/apps for conflict checks |
| `artifact_events` | Append-only artifact and workboard event log |
| `gate_tasks` | Workspace-owned frozen IDE-agent gate requests |
| `gate_runs` | Unified gate history for artifact and change-request subjects |
| `governance_feedback_events` | Coding-agent, user-reported check, delivery, Linear, and comment signals |
| `features` | Durable product capability records |
| `change_requests` | Work items; phase is derived, not persisted |
| `acceptance_criteria` | Normalized AC rows, including optional `verification_binding` |
| `workboard_lifecycle_events` | Workspace-owned Feature/CR lifecycle audit rows |
| `governance_threads` | Lightweight governance-chat sidebar index |
| `governance_files` | Internal upload blobs for explicit feature attachments |
| `documents`, `document_chunks`, `document_links`, `knowledge_chunks` | Governance Knowledge relational and vector stores |
| `integrations`, `integration_resources`, `integration_webhook_events`, `integration_delivery_links`, `tracker_links` | Native provider integration state |
| `settings` | Operator settings and encrypted secrets |
| `skills` | Team rubric Skills |
| `users`, `workspaces`, `workspace_members` | Local identity and workspace attribution |

`knowledge_chunks` is the one exception to migration authority: the pgvector
store creates it on demand when `KNOWLEDGE_DRIVER=pgvector`, so it does not
appear in the migration set.

### 3.2 `gate_runs`

`gate_runs` is the single evaluation history table.

| Column | Contract |
| --- | --- |
| `subject_kind` | `artifact` or `change_request` |
| `subject_id` | Artifact id or ChangeRequest id |
| `workspace_id` | Owning workspace; required |
| `gate` | Gate key such as `scope_clear` or `delivery_review` |
| `state` | `pass`, `warn`, `fail`, `needs_human_review`, `not_applicable`, `pending`, `not_run` |
| `executor` | `platform`, `human`, `ide_agent`, etc. |
| `evidence_json` | Gate evidence; workboard rows use `gate-run-v1` shape |

IDE-agent gate-task result rows use the `gate_tasks.id` as `gate_runs.id`, so a
submitted task result is also the persisted run. Their `evidence_json` envelope
preserves the task/result binding, trust, evaluator, submitted evidence, and
findings. For `spec_repo_drift`, submitted evidence includes `examined_docs`
and `repo_commit`.

## 4. Artifact Lifecycle

Artifact statuses:

| Status | Meaning |
| --- | --- |
| `draft` | Published but not approved |
| `approved` | Human-approved and eligible for downstream use |
| `needs_changes` | Human asked for changes; retained for audit |
| `superseded` | Replaced by a newer version for the same feature |

Transitions:

- publish creates `draft`;
- `draft -> approved` through status update with the frozen automatic policy;
- `draft -> needs_changes` requires a reviewer note;
- a newer approved/published version supersedes the previous active artifact for
  the same feature where applicable.

Approved artifacts are immutable. IDE/CLI agents publish a new artifact version
when content changes. Humans may review artifact decisions, but cannot author or
edit artifact bodies through the registry.

Version strings use `vMAJOR.MINOR`. A supplied `base_version` is an optimistic
lock: if it is not the latest version for the feature, publish returns 409.

## 5. Governance Files and Immutable Artifact Publishing

Governance files are internal upload blobs used by explicit feature attachments.
Browser and agent requests carry the selected `workspace_id`; repository reads
and mutations scope by that value. A workspace ID must be one safe opaque path
segment: separators, control characters, `.`, and traversal-like `..` values
are rejected before database or object-store writes. Browsers upload through `POST /governance/files/upload`
and read through `/content`; agents may use presign/commit when object storage is
reachable from their runtime. Presigned PUT requests sign the declared content
length, so callers cannot upload more bytes than the validated request. The API
does not expose object-store credentials.
Deleting an unreferenced governance file removes its local blob or S3 object
best-effort after the workspace-scoped database row is deleted. A file still
referenced by a feature attachment is rejected with 409; defensive object-key
reference checks also preserve any artifact row already pointing at that object.

New artifact bodies enter only through IDE/CLI publish. The registry has no
edit-session, proposal, or save-as-revision API; humans may read, approve,
reject, request changes, and review version history only.

Doc Registry is internal-open without HTTP authentication, so source kinds are
provenance rather than cryptographic caller identity. Strong IDE-only caller
enforcement requires a trusted CLI/IDE credential or signed ingress.

## 6. REST API

The normative route catalog and payload contracts live in the
[REST API contract](api.md). It is part of this specification. Routes are
registered with Huma v2 and return Huma problem details on error unless stated
otherwise.

## 7. Trust Boundary and Access Control

Doc Registry is an internal service. Do not add HTTP JWT/RBAC middleware unless
the architecture changes. The trust boundary is deployment/network level.

Client-provided actor fields (`created_by`, `approved_by`, `actor`,
`actor_kind`, `requested_by`) are audit attribution, not authentication.
`created_by` is the publishing client's proposer identity when available; CLI
publish sends the selected local username and server-side publish facades fall
back to their surface label only when the client omits it. Some endpoints enforce
cooperative role fields, for example Knowledge `source_of_truth` writes require
`actor_role` of `reviewer` or `admin` when the request provides it.

If the registry is exposed beyond the trusted network, front it with a proxy.
Keep internal settings routes private.

## 8. Policy Snapshots and Events

### 8.1 Governance Snapshot

Artifacts may carry `policy_snapshot_json`. New snapshots use
`snapshot_schema_version: "specgate.policy/v1"` and include:

| Field | Contract |
| --- | --- |
| `work_type` | `bugfix`, `change_request`, `new_feature` |
| `risk_level` | `low`, `medium`, `high` |
| `requested_governance_level` | Optional caller preference |
| `governance_level` | Enforced `light`, `standard`, or `enhanced` |
| `reason_codes` | Machine-readable policy reasons |
| `required_roles`, `required_topics`, `required_evidence` | Readiness obligations |
| `enabled_gates` | Gate keys to run |
| `approval_policy` | `human_required` |
| `evidence_policy` | `attested_ok` or `corroborated_required` |
| `gate_skills` | Gate key to Skill name map; navigation metadata after publication |
| `gate_definitions` | Frozen gate key/version and exact Skill content/digest used by both platform and IDE executors |
| `policy_lineage` | Built-in automatic policy lineage |
| `digest` | Snapshot digest |

Every non-empty snapshot must carry that version marker and both policy fields.
Approval fails closed when a snapshot is missing, unversioned, unparseable, or
incompatible.
The policy digest covers the frozen gate definitions. Editing a workspace Skill
therefore affects only artifact versions published afterward.
Publication fails if an automatic policy binds a Skill whose prompt cannot be
loaded; it must not silently replace a required team rubric with a default.

### 8.2 Artifact Events

Artifact lifecycle transitions append `artifact_events` rows.

Current event types:

- `artifact.published`
- `artifact.approved`
- `artifact.needs_changes`
- `artifact.superseded`
- `feature.canonical_changed`
- `change_request.acceptance_criteria_changed`
- `feature.stale_warning_added` (reserved)

`artifact.published`, status transitions, and their event rows must be committed
in the same transaction. See §14.

Every `artifact_events` row is chained for tamper-evidence: `prev_hash` carries
the `hash` of the artifact's previous event (empty for the first) and `hash` is
hex SHA-256 over the canonical string `prev_hash`, `id`, `artifact_id`,
`event_type`, `payload`, and `created_at` (RFC3339Nano, UTC, truncated to
microseconds — Postgres timestamptz precision), joined by newlines. The chain
head is read `FOR UPDATE` inside the writing transaction so concurrent writers
serialize. Every persisted event is chained; a missing or invalid hash is
tampering. Honest limits: deleting the newest events leaves a valid shorter
chain (no external anchor), and
verification runs server-side — it defends against direct database edits, not a
compromised server binary.

## 9. Retention and Cleanup

Artifact retention windows:

| Status | Automated cleanup |
| --- | --- |
| `approved` | Never auto-deleted |
| `draft` | Never auto-deleted |
| `superseded` | Eligible after 90 days |
| `needs_changes` | Eligible after 30 days |

The retention sweeper is opt-in through `retention.artifact_sweep_enabled` or
`ARTIFACT_RETENTION_SWEEP_ENABLED=true`. The sweeper skips artifacts referenced
by any Feature canonical pointer or ChangeRequest lead artifact. If reference
lookup fails, cleanup fails closed. Artifact deletion locks and rechecks the
current workboard references; creation of an exact-version work binding holds a
shared artifact lock, so cleanup cannot race a new binding into a dangling
reference.

`POST /maintenance/cleanup` runs the retention sweep immediately, removes fixed
demo seed rows, and purges archived ChangeRequests within the selected workspace.
It never deletes approved or draft artifacts (other than the orphaned standalone
packs above), active Features, non-archived work items, audit events, or another
workspace's integration-derived delivery and tracker links.
The archived-work purge requires a selected workspace even below the HTTP layer;
there is no global cleanup path.

Governance files have separate TTL cleanup controlled by
`governancefiles.ttl_days` (default 90). Ready files referenced by feature
attachments are pinned; defensive object-key checks also preserve any existing
artifact reference. Pending presigns older than one hour are orphan cleanup
candidates. The periodic cleanup runs against the configured storage driver:
local stacks delete local blobs and S3 deployments delete S3 objects.

## 10. Conflict Detection

Conflict detection is advisory. It checks overlapping impacted services/apps
among active artifacts and returns:

| State | Meaning |
| --- | --- |
| `no_conflict` | No active overlap |
| `warning_conflict` | Overlap that does not block review |
| `blocking_conflict` | Active overlap with draft or approved work |

Publish still proceeds unless the caller chooses to stop.

## 11. Delivery and Integration Signals

Delivery evidence is recorded as `governance_feedback_events` and `gate_runs`.
The delivery reviewer writes a `delivery_review` gate run. Human delivery
decisions also write `delivery_review` runs with `executor=human` and
`trust=human_decision`. A human decision outranks platform reruns bound to the
same `completion_feedback_event_id`; a different later completion starts a new
review-and-decision cycle. When a platform review exists, the human run
preserves its completion binding, nested criterion/check evidence,
`evidence_verdict`, evidence judge/evaluation version, and evidence confidence
while replacing authority, verdict, actor, and note. The write uses the exact
reviewed gate-run ID and completion-event ID as a compare-and-swap under the
change-request lock. Review persistence and completion persistence take the same
lock, so a newer cycle cannot land between validation and the decision write.
The governance operation rejects a decision without a current platform review,
against an older completion, or after another decision for the same completion.

`GET /api/v1/work-items/{id}/delivery-status` returns the authoritative
decision in `verdict` and, for human runs, the reviewed platform assessment in
optional `evidence_verdict`. `reason_code=policy_unavailable` is the
deterministic fail-closed policy guard.
Optional `assurance_sources` is derived only from persisted server-observed
delivery evidence. `repository_observed` means a merged-PR/MR event whose
normalized payload `head_sha` equals the latest completion receipt's normalized
`git_receipt.head_revision`; missing, mismatched, or stale events do not
corroborate. Repository observation does not assert that checks passed and is
not a human decision. Users may still cite their own test or CI results in a
completion report.
`reason_code=delivery_review_outdated` means the newest completion has no
matching review yet; its Git receipt may be returned for inspection, but no
actor or reviewed gate-run ID is claimed.

Once a completion is human-accepted, another completion for that work item is
rejected. Further implementation starts as a new work item so archive and
tracker-close side effects cannot become stale.

Archive and linked-tracker close transitions occur only after a human approval.
A platform pass means ready for human review and has no terminal side effects.

Evidence policies:

| Policy | Pass requirement |
| --- | --- |
| `attested_ok` | Agent report plus green checks may pass |
| `corroborated_required` | Pass requires a merged-PR/MR event bound to the latest completion head or all criteria resolved by canonical deterministic bindings |

Agent-supplied `source` values are not trusted. CLI feedback is agent-reported;
provider webhook paths stamp `webhook` provenance. A `coding_agent.completed`
event requires a non-empty `agent.name`. CLI local evidence grounding may add
digest/excerpt metadata and produce `grounded` trust.

Peer review remains cooperative: `coding_agent.peer_reviewed` must bind the
latest completion event and its exact Git receipt, come from a different named
agent, and cover every canonical acceptance criterion exactly once. It cannot
approve delivery by itself.

Provider delivery events:

- PR/MR opened, merged, closed become provider-neutral `delivery.pr_*`
  feedback;
- tracker status/comment events may emit `delivery.tracker_status_changed` or
  `delivery.comment_scope_drift`;
- tracker lifecycle uses provider-normalized structured state; labels and prose
  never establish `opened`, `closed`, or `removed`;
- exact `<!-- specgate-work-ref: {key|id} -->` markers are required for PR/MR matching.

## 12. Errors

Huma routes return RFC 9457 problem details. Important mappings:

| Status | Common cause |
| --- | --- |
| `400` | Validation error |
| `401` | Webhook auth failure |
| `404` | Unknown artifact, CR, resource, or route not registered |
| `409` | Version conflict, stale base, incompatible policy snapshot |
| `413` | Webhook or Knowledge body too large |
| `422` | Huma request binding/schema error or stale gate digest |
| `500` | Internal error |
| `503` | Required service not configured, such as agents or Skills |

## 13. Implementation Notes and Observability

The Go module owns one REST process with optional workers and scheduled cleanup.

Sentry is optional. `SENTRY_DSN` enables reporting; `SENTRY_ENVIRONMENT` defaults
to `development`, `SENTRY_RELEASE` is optional, and
`SENTRY_TRACES_SAMPLE_RATE` defaults to `0`. Panic reporting is wired through
middleware, and request IDs are the primary correlation key because the HTTP
surface has no authentication identity.

Redis-backed queues are optional. Without Redis, webhook and Knowledge ingestion
run synchronously/inline where their contracts allow it.

When Redis is enabled, webhook and Knowledge ingest task payloads carry the
workspace derived from their stored parent. Workers pin that workspace before
re-reading the parent and reject missing or mismatched ownership. New artifact,
Knowledge, and governance-upload object keys are workspace-prefixed. Knowledge
derives its key prefix from the document's stored workspace ownership, so a
missing request context cannot create an unscoped product object. pgvector
upserts and searches require a non-empty workspace filter. Stored-object reads
are bounded by their declared size and owning product limit; Knowledge and
governance-file byte-limit configuration must be positive.
Each Knowledge publication writes its raw source under a unique attempt key and
persists that exact key as `source_uri`, so cleanup after a conflicting
publication cannot remove the winning version's bytes. Enqueue failures and
errors at every ingest stage transition the persisted version to `failed` with
the error recorded; asynchronous workers may subsequently retry from that state.

## 14. Transaction and Side-Effect Rules

Artifact row writes and artifact event writes are transactional:

- publish creates the artifact, files, service rows, and `artifact.published`
  event together;
- status update writes the artifact status and transition event together;
- artifact deletion removes database rows transactionally and deletes object
  files best-effort;

Side effects that may cross stores or providers are best-effort after the durable
state change unless explicitly specified otherwise. Examples:

- Knowledge version deletion removes the workspace-scoped metadata first, then
  removes its vectors and raw/processed objects best-effort. Search verifies
  each vector hit still has matching workspace-owned metadata in `indexed`
  status before returning it. An ingest worker that discovers the metadata was
  concurrently deleted makes one final best-effort cleanup of processed objects
  and vectors before stopping;
- feedback event resolution;
- Feature `planned -> active` promotion after an approved artifact version;
- tracker issue closure after explicit human delivery approval;
- upstream provider webhook deletion during resource delete, where strict
  endpoint behavior returns 502 and leaves local resource untouched on upstream
  failure.

Comments that reference `per spec §14` should point to this section.

## 15. Artifact-Native Work Board

Doc Registry owns durable workboard records. Board phase is derived on read and
never persisted as a separate status enum.

Phase derivation:

| Phase | Condition |
| --- | --- |
| `Intake` | No governance thread and no lead artifact, except quick-route bug-fix work |
| `Draft` | Governance thread exists but no lead artifact |
| `Review` | Lead artifact exists but is not approved |
| `Ready` | Approved lead artifact, or quick-route bug-fix work with no lead artifact |
| `Delivered` | Current completion has an authoritative human delivery approval |

`Delivered` overrides artifact-derived phase. Human delivery decisions outrank
platform reruns for the same completion. A later completion starts a new cycle;
within one executor tier and cycle, newest wins.

When a ChangeRequest points to a known governance-chat thread, that thread must
belong to the same workspace on both create and update. A not-yet-indexed thread
ID remains valid so LangGraph can create its sidebar entry after the work item.

ChangeRequest list/read DTOs expose derived fields:

- `phase`;
- `tracker_status`;
- `delivery_review` with `{verdict, hint, reviewed_at, executor?, actor?, note?, summary?}`.

Stale warning codes:

- `canonical_artifact_missing`
- `canonical_artifact_unapproved`
- `canonical_artifact_superseded`
- `canonical_promotion_available`
- `lead_artifact_superseded`
- `linked_knowledge_newer`
- `feature_deprecated`
- `delivery_in_progress`
- `tracker_status_conflict`
- `tracker_priority_urgent`
- `delivery_stale`

`delivery_stale` fires when the current cycle's authoritative delivery review is a failing run
older than `DELIVERY_SLA_DAYS` (default 7). It does not fire for missing review
history, passing authoritative review, same-cycle human decisions, or failures
inside the SLA window.

Quick-route work may be featureless. Featureless CRs must not cause the server
or UI to invent a durable Feature.

## 16. Native Integrations

Native integrations are durable provider records. Webhook callbacks are
resource-scoped, and each selected resource owns its encrypted signing secret.
All normal integration catalog, lifecycle, child-resource,
credential, and OAuth calls require the selected workspace. Provider callbacks
derive ownership from the named integration and do not accept caller-supplied
workspace input.

Provider contracts:

| Provider | Inbound auth | Main signals |
| --- | --- | --- |
| GitLab | Standard Webhooks `webhook-signature` | merge requests, comments |
| GitHub | `X-Hub-Signature-256` HMAC | pull requests, issue comments |
| Linear | `Linear-Signature` HMAC, signed `webhookTimestamp` freshness (±1 minute), and `Linear-Delivery` dedup id | issue status, comments, deletes |

Webhook processing authenticates synchronously. With Redis configured, accepted
deliveries enqueue secret-free work; without Redis, the same normalize/match/
commit pipeline runs inline. Dedup uses provider delivery ids when present and
`sha256:` payload hashes when not. Processed and in-flight duplicates are
no-ops. A row whose prior transaction failed is atomically reclaimed as pending
so one queue redelivery may retry it; concurrent redeliveries cannot both create
links or feedback.

Resource types are provider-specific: GitHub uses `repo`, GitLab uses `project`,
and Linear uses `team`. Mismatched pairs are rejected before persistence.
Resource creation auto-provisions GitLab/GitHub/Linear webhooks when outbound
auth and matching provider resource data are available. If local secret or
configuration persistence fails after provisioning, the managed upstream
webhook is best-effort deleted before the new resource is rolled back.
Reprovisioning also best-effort deletes its newly created upstream webhook when
post-provision local persistence fails, while preserving the existing resource
and best-effort recording an error state. Resource deletion deletes the managed
upstream webhook first; integration deletion does the same for every selected
resource. Upstream not-found is treated as already removed, while other upstream
failures preserve the local rows for retry.

Provider matching is explicit:

- tracker links match by persisted provider ids/keys before marker fallback;
- PR/MR links require `<!-- specgate-work-ref: {key|id} -->`;
- branch, URL, title, or body mentions without the exact marker do not link
  delivery work.
