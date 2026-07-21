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

Routes are registered with Huma v2. Unless noted, routes are internal-open and
return Huma problem details on error. New endpoints must be added here and in
`internal/api/router.go`.

### 6.1 Core Route Catalog

System:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Liveness; routine probes are omitted from request logs |
| `GET` | `/api/v1/schema/status` | Read-only database schema compatibility diagnostic for required columns and workspace ownership constraints |
| `GET` | `/readyz` | Readiness; routine probes are omitted from request logs |
| `POST` | `/maintenance/cleanup` | Retention sweep, demo seed removal, archived CR purge |
| `POST` | `/maintenance/demo-remove` | Remove fixed demo seed rows |

Artifacts and files:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/artifacts` | List artifacts |
| `GET` | `/artifacts/{id}` | Read artifact metadata |
| `PATCH` | `/artifacts/{id}/status` | Approve or request changes |
| `GET` | `/artifacts/{id}/files` | List artifact files |
| `GET` | `/artifacts/{id}/files/_?path=...` | Read one explicit immutable UTF-8 document (maximum 1 MiB) |
| `GET` | `/conflicts` | Advisory service/app overlap check |
| `GET` | `/events` | Poll artifact/workboard events |

All Artifact and artifact-file routes require the selected `workspace_id`.
The service binds it before resolving any artifact or governance-file reference;
a row from another workspace is indistinguishable from an unknown row.
Workspace-scoped projections also require every denormalized child row to agree
with its parent workspace before it is returned.

Feature attachments and governance files:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/features/{id}/attachments` | Pin feature-scoped link/file/image reference |
| `GET` | `/features/{id}/attachments` | List feature references |
| `DELETE` | `/attachments/{id}` | Delete a feature reference |
| `POST` | `/governance/files/upload?workspace_id=...` | Browser-safe upload through API |
| `POST` | `/governance/files/presign` | Agent presigned upload (`workspace_id` in body) |
| `POST` | `/governance/files/{id}/commit?workspace_id=...` | Commit presigned upload |
| `GET` | `/governance/files?workspace_id=...` | List ready governance files |
| `GET` | `/governance/files/{id}/content?workspace_id=...` | Stream file through API |
| `POST` | `/governance/files/{id}/touch?workspace_id=...` | Refresh TTL usage |
| `DELETE` | `/governance/files/{id}?workspace_id=...` | Delete an unreferenced file row and object (409 when pinned) |

Settings, Skills, and threads:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` / `PUT` | `/settings` | Operator key/value settings |
| `GET` / `POST` | `/skills` | List/create workspace-owned Skills (`workspace_id` query/body) |
| `PUT` / `DELETE` | `/skills/{id}` | Replace/delete Skill (`workspace_id` query) |
| `GET` | `/governance/threads?workspace_id=...` | List thread summaries within one workspace |
| `PUT` | `/governance/threads/{thread_id}` | Upsert a thread summary; body requires `workspace_id` |
| `DELETE` | `/governance/threads/{thread_id}?workspace_id=...` | Archive one workspace-owned thread summary |

Workboard:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` / `POST` | `/workboard/features` | List/create Features; selected `workspace_id` is required in query/body |
| `POST` | `/workboard/features/upsert-by-key` | Idempotent create-or-get Feature; `workspace_id` is required in body |
| `GET` / `PATCH` | `/workboard/features/{id}` | Read/update Feature; `workspace_id` is required in query. Set status to `deprecated` instead of deleting governed history |
| `POST` | `/workboard/artifacts/{id}/promote-canonical` | Promote an approved artifact to its feature's canonical; `workspace_id` is required in query (non-approved → 400; featureless → 400) |
| `GET` / `POST` | `/workboard/change-requests` | List/create ChangeRequests; selected `workspace_id` is required in query/body. A supplied lead artifact must be approved or superseded and belong to the same Feature and workspace |
| `GET` / `PATCH` | `/workboard/change-requests/{id}` | Read/update ChangeRequest; `workspace_id` is required in query |
| `GET` / `POST` | `/features/{id}/attachments?workspace_id=...` | List/create feature reference attachments inside one workspace |
| `DELETE` | `/attachments/{id}?workspace_id=...` | Delete one workspace-owned feature attachment |
| `POST` | `/workboard/change-requests/{id}/unarchive` | Restore archived CR |
| `GET` | `/workboard/change-requests/{id}/acceptance-criteria` | List normalized AC rows |
| `GET` | `/workboard/change-requests/{id}/delivery-links?workspace_id=...` | List persisted repository delivery links for one work item; submitted `head_sha` and provider `merge_commit_sha` remain distinct |
| `GET` | `/workboard/change-requests/{id}/next-actions` | Derived gate actions |
| `POST` | `/workboard/change-requests/{id}/gate-runs/refresh` | Persist a gate snapshot. `evaluations_only=true` persists only supplied evaluations, used by delivery review after the quality-gate refresh to avoid duplicate deterministic history |
| `GET` | `/workboard/change-requests/{id}/gate-runs` | List gate snapshots |
| `GET` | `/workboard/change-requests/{id}/tracker-links` | List tracker links |
| `POST` | `/workboard/change-requests/{id}/linear-handoff?workspace_id=...` | Create or return the selected-team Linear issue for a Ready work item |
| `GET` | `/workboard/stale-warnings` | List stale/context/delivery warnings |

All remaining ChangeRequest detail, link, acceptance-criteria, next-action,
gate-run, tracker-link, and stale-warning calls also require the selected
`workspace_id` query value. Missing context returns `400`; a known row from
another workspace returns `404`, matching an unknown row. Provider callbacks
derive ownership from their named integration. Feedback and lifecycle audit rows
store a required workspace owner, including agent-originated feedback without an
integration.

Knowledge:

Every Knowledge route carries the selected `workspace_id` (query, JSON body,
or upload form). List, count, search, detail, version, retry, curation, delete,
and retrieval-citation repository operations are pinned to that trusted request context, so
an ID from another workspace is not readable or mutable even when a caller
reaches the service below the HTTP precheck.
Caller-supplied `document_id` values are opaque safe path segments: separators,
control characters, `.`, and traversal-like `..` values are rejected before
object or metadata persistence.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/documents/upload` | Upload Knowledge file version |
| `POST` | `/documents/text` | Create Knowledge text version |
| `GET` | `/documents` | List Knowledge documents (`items` is the requested page; `total` is the full matching count before `limit`/`offset`) |
| `GET` | `/documents/{document_id}` | Read document detail/history |
| `POST` | `/documents/{document_id}/versions` | Add text version; request body uses the configured Knowledge size envelope |
| `POST` | `/documents/{document_id}/links` | Create linked metadata version |
| `POST` | `/documents/{document_id}/retry` | Re-ingest failed version |
| `DELETE` | `/documents/{document_id}` | Delete one version |
| `POST` | `/governance/context/search` | Search Knowledge context |

Integrations:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` / `POST` | `/integrations?workspace_id=...` | List/create workspace-scoped integrations; missing workspace returns `400` |
| `GET` / `PUT` / `DELETE` | `/integrations/{id}?workspace_id=...` | Read/update/delete integration within the selected workspace; missing workspace returns `400` |
| `PUT` | `/integrations/{id}/api-token` | Set provider PAT |
| `GET` | `/integrations/{id}/repos` | List provider repos |
| `GET` | `/integrations/{id}/linear/teams` | List Linear teams |
| `GET` | `/integrations/{id}/linear/projects` | List Linear projects |
| `GET` / `POST` | `/integrations/{id}/resources` | List/create resources |
| `DELETE` | `/integrations/{id}/resources/{resource_id}` | Delete resource and managed webhook |
| `POST` | `/integrations/{id}/resources/{resource_id}/reprovision-webhook` | Recreate resource webhook |
| `GET` / `POST` | `/integrations/{id}/webhook-events` | List/record webhook inbox rows |
| `POST` | `/integrations/{id}/resources/{resource_id}/gitlab/webhook` | Resource GitLab receiver |
| `POST` | `/integrations/{id}/resources/{resource_id}/github/webhook` | Resource GitHub receiver |
| `POST` | `/integrations/{id}/resources/{resource_id}/linear/webhook` | Resource Linear receiver |
| `POST` | `/integrations/oauth/begin` | Begin create-on-callback OAuth |
| `POST` | `/integrations/{id}/oauth/authorize` | Begin OAuth re-auth |
| `GET` | `/integrations/oauth-callback` | OAuth callback |
| `POST` | `/integrations/{id}/oauth/disconnect` | Remove managed provider webhooks, clear their local secrets, then clear the OAuth grant; provider cleanup failure preserves the grant for retry |
| `GET` | `/governance/feedback-events` | List workspace-owned feedback events (`received`, `accepted`, or `rejected`), optionally filtered by exact `change_request_id`, `artifact_id`, or `event_type` |
| `POST` | `/governance/feedback-events/{id}/status` | Resolve a feedback event as `accepted` or `rejected` |
| `GET` | `/repos/file?workspace_id=...` | Read a GitLab integration-backed repo file in the selected workspace; metadata and actual decoded/raw content are bounded, with a 1 MiB file limit |

Linear handoff accepts `integration_id` and an exact `resource_id` for a connected
Linear team in the selected workspace. It is idempotent per change request: the
first call creates one issue and tracker link; later calls return that link with
`created: false`. The issue is rendered from persisted title, intent, ordered
acceptance criteria, and the exact `specgate work context <key> --json` and PR/MR
marker instructions. A deterministic caller-selected Linear issue ID permits
recovery after an ambiguous provider response or local persistence failure. The
selected team resource is retained by `tracker_links.resource_id` and is used for
the best-effort Done transition after human delivery acceptance. Recovered and
created issue responses must identify that same selected team before a link is
stored; transition errors never prevent that acceptance.

Integration creation, including pending OAuth connection state, requires the
trusted selected workspace before any row is stored.

Outbound provider clients use request timeouts and bounded response bodies:
GitHub, GitLab, and Linear GraphQL responses are capped at 4 MiB; OAuth identity
responses are capped at 1 MiB. Successful close/delete calls do not buffer
unused bodies.

Integration child rows (resources, OAuth state, webhook events, delivery and
tracker links, credentials, and integration-backed feedback) derive ownership
from their parent integration. Scoped requests reject a child from another
workspace as not found. Provider callbacks authenticate the named integration,
then bind its workspace before any resource lookup, work-item matching, inbox
write, or delivery-link write. Work keys repeated in another workspace therefore
cannot be matched by that callback. OAuth callbacks likewise bind the workspace
stored in their one-time state before reading or updating an integration.
Feedback with no integration id stores the trusted agent workspace directly and
appears only in that workspace's catalog.

Only connected integrations accept provider callbacks. Disconnecting OAuth
disables the integration before any later signed delivery can enter the inbox.
Deleting an integration removes each managed upstream webhook before deleting
the local integration graph; an upstream cleanup failure preserves the local
credentials and resource metadata so the operation can be retried.

Artifact publication and gate tasks:

IDE agents submit complete new artifact versions. Existing artifact snapshots
are immutable: there is no edit-session, proposal, or save-as-revision API.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/artifacts/{artifact_id}/gate-preview?workspace_id=...` | Preview gate tasks inside one workspace; preview rows do not fabricate persisted-run notes |
| `POST` | `/api/v1/artifacts/{artifact_id}/gate-tasks?workspace_id=...` | Dispatch gate tasks inside one workspace |
| `GET` | `/api/v1/gate-tasks?workspace_id=...` | List pending gate tasks |
| `GET` | `/api/v1/gate-tasks/{task_id}?workspace_id=...` | Read one workspace-owned task |
| `POST` | `/api/v1/gate-tasks/{task_id}/result?workspace_id=...` | Submit one workspace-owned task result |

### Gate-task binding and no-model dispatch

Each dispatched task binds `artifact_digest` to the published artifact's
immutable `snapshot_digest`, along with its frozen gate key/version/digest,
policy digest, rubric content, workspace, executor, and expiry. A result must
submit the same gate key and both digests; mismatched identity, stale input
digests, and expired tasks are rejected. The registry stamps evaluator trust,
records `input_digest` in the gate-run evidence, and keeps one result per task:
a later valid submission replaces the prior result.

Draft artifacts dispatch their content gates but defer `spec_repo_drift` until a
human approves the artifact. Dispatch returns `created_task_ids`,
`skipped_gate_keys`, and all current `pending_task_ids`. Pending lists omit
expired unsubmitted tasks. Dispatch replaces only expired-unsubmitted work;
completed matching work remains idempotent even after its task expiry. In
no-model readiness, non-empty `pending_task_ids` retains a `not_run` aggregate
unless an existing `fail` or `needs_human_review` is stronger.
Once tasks complete, readiness responses and canonical-promotion audit metadata
aggregate the latest persisted result per gate across platform and IDE-agent
executors. IDE-agent results therefore affect readiness without being presented
as platform evaluation; human decisions remain separate. A later deterministic
or platform result for the same gate supersedes an older result by timestamp.

Current-task detection and creation are one atomic store operation keyed by
workspace, artifact/input digest, gate key, and gate digest. Concurrent
dispatch requests can create at most one current task for that identity.

### 6.2 Publish Artifact

Only the IDE/CLI facade publishes artifacts: `POST /api/v1/artifacts/publish`
with a required `workspace_id` query scope. It accepts the following complete
immutable-version shape:

```json
{
  "feature_key": "checkout-loyalty",
  "workspace_id": "workspace-uuid",
  "request_type": "change_request",
  "impact_level": "medium",
  "base_version": "v0.2",
  "documents": [
    {"path": "spec.md", "role": "spec", "content": "# Complete spec text"}
  ],
  "created_by": "thanhtung2693",
  "source_kind": "codex",
  "source_id": "thread-or-run-id",
  "authority": "product_intent",
  "requested_governance_level": "standard",
  "impact_declaration": {}
}
```

The generic `POST /artifacts` route is intentionally absent: the review UI can
read versions and record approval/request-changes decisions, but cannot author
or publish artifact content. Publish stores exact normalized repository-relative document paths and
server-computed `content_sha256` values. It also stores `snapshot_digest`, a
deterministic digest of sorted path/role/content-hash entries. Blob keys are
artifact-ID scoped so conflicting publishes cannot overwrite another version's
bytes. Existing `source_kind`, `source_id`, `source_revision`, and `created_by`
fields record provenance; document hashes identify exact captured bytes.
Updates from an existing selected package must set `base_version` and create a
new version.

Artifact publish accepts explicit `documents[]` only. Every document supplies
its repository-relative path, role, and inline content. Governance-file or
object-store references are not publish inputs. An explicit empty inline value
publishes a zero-byte document rather than dropping the declared path.

Publish is non-blocking on required document roles: the draft always lands, and
the response reports any gap via `missing_roles` (the resolved automatic
policy's required roles absent from the published documents) plus a human-readable
`readiness_hint`. Missing roles later surface as a `required_roles_present`
readiness warn; they never fail the publish itself.

Every publish stays `draft`, including role-complete packages. A human records
the approval or request-changes decision after reviewing the immutable version.

`workspace_id` is accepted on the publish body; the CLI annotates publish
requests with the selected workspace. Feature, Artifact, and ChangeRequest roots
store workspace ownership. Normal WorkBoard Feature/ChangeRequest list/get/
update/gate paths require a non-empty workspace; missing context returns `400`,
and wrong-workspace IDs return not found, matching unknown IDs. Provider
callbacks derive ownership from their named integration; feedback rows always
store a workspace. Publishing a feature-backed Artifact validates that the linked
Feature exists in the same workspace before writing.

### 6.3 Status Update

`PATCH /artifacts/{id}/status` uses:

```json
{
  "status": "approved",
  "approved_by": "reviewer@example.com",
  "review_rating": 3,
  "note": "Minor edits accepted",
  "actor_kind": "human"
}
```

`actor_kind` defaults to `human`. Agent approval is always rejected.
Identity is client-asserted: this provides audit attribution, not
identity-secure authorization. Empty or incompatible snapshots, including
retired approval policies, fail closed for approval. Status-transition events include
`actor_kind` in the JSON payload so audit readers can distinguish a human
approval from an agent or service transition.
The status endpoint never changes artifact files or manifests; publish a new
version for any artifact-content change.

### 6.4 Conflicts

`GET /conflicts?services=` is advisory. It checks active artifacts (`draft` and
`approved`) with overlapping impacted services/apps. It never blocks publish by
itself and never changes stored lifecycle state. Each candidate and existing
artifact reference contains only `feature_id`, `version`, and `status`.

### 6.5 Settings

`GET /settings` and `PUT /settings` manage global server string key/value
settings. They are not workspace-owned; workspace selection must not alter the
settings catalog. Unknown keys are rejected by the fixed settings whitelist.

Secrets are encrypted when stored and masked as `***` on browser reads. Trusted
governance-service reads use `X-SpecGate-Internal-Agent: governance`. PUT with
`***` preserves the existing secret. The public Local and Full gateways strip
that internal header; only the service-to-service governance client on the
internal network can request unmasked values.

Important settings:

| Key | Contract |
| --- | --- |
| `governance.model_provider`, `governance.model`, `governance.default_thinking_level` | App model settings |
| `embedding.model_provider`, `embedding.model` | Knowledge embedding settings |
| `openai.api_key`, `google.api_key`, `anthropic.api_key`, `openrouter.api_key` | Provider secrets |
| `governance.auto_archive_on_delivery_pass` | Archive CR after an explicit human delivery approval |
| `governance.feature_freshness_sla_days`, `governance.artifact_stale_days` | UI/read-model stale thresholds |
| `governance.gate_confidence_threshold` | Model gate confidence floor |
| `retention.artifact_sweep_enabled` | Artifact retention sweeper toggle |
| `governancefiles.ttl_days` | Governance-file TTL |

Governance providers use the supported provider enum and model IDs are required,
non-empty provider-owned identifiers. SpecGate does not infer provider
compatibility from model-name prefixes; the provider call validates existence
and capability.

### 6.6 Skills API

Root `/skills` uses a Huma response envelope named `body` and requires
`workspace_id` in collection queries, create bodies, and ID mutations. The
`/api/v1/skills` facade returns top-level fields for CLI clients and also
requires `workspace_id`. A Skill has `id`, `workspace_id`, `name`,
`description`, `prompt`, `created_at`, and `updated_at`; custom names are unique
within one workspace. Development data must assign every row before final
ownership constraints are enabled.

Workspace ownership is mandatory and non-blank on every workspace root. The development
schema is fresh-install only: there is no ownership backfill or legacy
compatibility path. Startup rejects an older database before it can serve
requests. Derived reads, including artifact-backed Context Packs, verify that
their resolved root belongs to the trusted workspace and, for a ChangeRequest,
to that ChangeRequest's workspace before any readiness or file read.

Automatic policy snapshots bind Skills to gates through `gate_skills`.
Readiness and delivery judges inject the bound Skill prompt as team rubric text.

### 6.7 CLI REST Facades

`/api/v1/*` routes are the CLI facade. They delegate to
`governanceops.Service`; business logic must stay out of the handlers.
Workspace-owned facade requests require `workspace_id`; the bound workspace
scopes all reads and writes, and a body workspace cannot override it. Discovery,
identity, and workspace-management routes are exempt. `GET /api/v1/status` and
`GET /api/v1/stats` may instead use `all_workspaces=true` for an explicit
cross-workspace read; no write or other facade route accepts that substitute.
This includes artifact and work-item policy explanations, which resolve their
backing artifacts through the bound workspace.
Feature-backed work creation also verifies that the feature's canonical artifact
belongs to the same workspace before persisting a ChangeRequest reference.

Important facades:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/meta` | Server version, recommended CLI version, configured `web_url`, and typed `capability_details` states (`available`, `unavailable`, `configuration_required`) with no secret values |
| `GET` | `/api/v1/status` | Governance status counts and attention |
| `GET` | `/api/v1/stats` | Rolling governance-value metrics |
| `POST` | `/api/v1/identity/bootstrap` | Create/reuse local user and workspace, then idempotently install missing built-in rubric Skills before returning |
| `GET` | `/api/v1/users`, `/api/v1/users/{id}` | Local users |
| `GET` | `/api/v1/workspaces`, `/api/v1/workspaces/{id}` | Local workspaces |
| `GET` | `/api/v1/workspaces/{id}/members` | Read workspace members with optional current-user marker |
| `POST` | `/api/v1/work-items/resolve` | Resolve CR key/id or tracker ref |
| `GET` | `/api/v1/work-items/{id}/context-pack` | Assemble Context Pack |
| `POST` | `/api/v1/work-items/{id}/feedback` | Record coding-agent feedback |
| `POST` | `/api/v1/work-items/{id}/readiness` | Agents-backed readiness |
| `POST` | `/api/v1/work-items/{id}/llm-gates` | Agents-backed quality gates |
| `POST` | `/api/v1/work-items/{id}/delivery-review` | Agents-backed delivery review |
| `POST` | `/api/v1/work-items/{id}/delivery-decision` | Human delivery approve/reject |
| `POST` | `/api/v1/work-items` | Create quick-route work item |
| `POST` | `/api/v1/work-items/create` | Create feature-backed work item bound to the feature's canonical artifact; body accepts `feature` (key or id), `title`, `description`, a non-empty explicit `acceptance_criteria[]`, and optional structured `source_refs[]` provenance. A criterion may end in a human-authored `@check:<name>` token; the token is removed from its text and persisted as `verification_binding`. Trusted portable import may also pass `artifact_id`; the server accepts only an approved or superseded artifact from that feature and workspace so older exact bindings survive a newer canonical version. The IDE agent reads the mapped source specs and supplies the confirmed list; the server does not infer criteria from document structure or keywords. Feature without a canonical artifact is rejected (approve + promote first) |
| `POST` | `/api/v1/work-items/{id}/archive` | Archive CR |
| `GET` | `/api/v1/work-items/{id}/gates` | Read current gate state |
| `GET` | `/api/v1/work-items/{id}/gate-history` | Read gate run history |
| `GET` | `/api/v1/work-items/{id}/delivery-status` | Read authoritative delivery review. Optional `assurance_sources` reports server-observed `repository_observed` corroboration independently of per-criterion trust and human acceptance. `detail=true` includes the latest completion receipt and informational `peer_review` state: `not_run`, `passed`, `failed`, or `stale`. A peer state is evidence only and never replaces a human delivery decision. |
| `GET` | `/api/v1/work-items/{id}/policy` | Explain work-item policy |
| `GET` | `/api/v1/audit/{ref}` | Chronological governance audit trail for a work ref |
| `POST` | `/api/v1/artifacts/publish` | CLI/IDE artifact publish; the shared Local/Full boundary caps each inline document at 1 MiB and the decoded package at 10 MiB |
| `GET` | `/api/v1/artifacts`, `/api/v1/artifacts/{id}` | Artifact read surfaces |
| `GET` | `/api/v1/artifacts/{id}/files`, `/files/_?path=...` | Artifact file read surfaces |
| `GET` | `/api/v1/skills?workspace_id=...`, `/api/v1/skills/{id}?workspace_id=...` | Workspace-scoped Skill catalog/detail |
| `GET` | `/api/v1/policies/levels` | Built-in policy levels |
| `POST` | `/api/v1/policies/resolve` | Policy dry-run |
| `GET` | `/api/v1/artifacts/{id}/policy` | Explain artifact policy |

Stats are computed from existing gate runs, feedback events, and CR rows. No
separate stats collection table exists.

Workspace member responses are read-only cooperative identity surfaces backed by
`workspace_members` joined to `users`. The `{id}` path may be a workspace id or
slug. Optional `current_user_id` / `current_username` query values mark the
matching member row with `current: true`; they do not authorize access, create
membership, or mutate identity state. The response shape is:

```json
{
  "workspace": {"id": "ws_...", "slug": "specgate", "name": "SpecGate"},
  "current_user": {"id": "user_...", "username": "thanhtung2693"},
  "members": [
    {
      "workspace_id": "ws_...",
      "user_id": "user_...",
      "username": "thanhtung2693",
      "display_name": "Thanh Tung",
      "email": "",
      "role": "owner",
      "current": true
    }
  ]
}
```

### 6.8 Audit Trail

`GET /api/v1/audit/{ref}` returns the full chronological governance history for a
work reference — the "git log for governance". `ref` resolves via the same
semantics as `/api/v1/work-items/resolve` (a change-request id or key such as
`CR-1234`); an unresolvable ref returns `404`. The endpoint is read-only and
merges five read-only sources for the resolved CR and its feature:

1. **Artifact status events** (`artifact_events`) for each artifact in the
   feature lineage (lead + canonical + context-pack) → `published` /
   `approved` / `needs_changes` / `superseded`. Actor + `actor_kind` come from
   the event payload.
   With `?verify=true` the response also carries `chain`
   `{state: intact|tampered, artifact_id?, first_bad_event_id?, chained_events}`
   — the recomputed tamper-evidence verdict across the lineage artifacts (worst
   state wins; see §8.2).
2. **Artifact readiness runs** (per artifact) → action `gate:<name>`, verdict =
   state, `trust` derived from the executor.
3. **CR gate runs** → quality gates as `gate:<name>`; historical
   `delivery_review` runs as action `delivery_review`.
4. **Workboard lifecycle events** (`workboard_lifecycle_events`) for both the
   feature and the CR → action = event type.
5. **The authoritative (latest) `delivery_review` snapshot** → action
   `delivery_review` (the latest run is folded from here for its richer
   actor/note and deduped against source 3).

Configured source failures fail the audit request instead of returning a
partial history that looks complete. `verify=true` also requires the artifact
event reader whenever the work item has lineage artifacts; it never reports an
intact chain that it could not read. A referenced artifact with no event rows is
reported as `tampered`, because every published artifact creates a chained
`artifact.published` event.

`trust` is **derived at read time** from the gate-run executor — there is no
`trust_tier` column: `human → human`, `ide_agent → agent_attested`,
`platform → platform`, and empty when no executor (artifact status and lifecycle
events).

Response `AuditTrail`:

```json
{
  "ref": "CR-1234",
  "change_request_id": "cr-...",
  "change_request_key": "CR-1234",
  "feature_id": "feat-...",
  "feature_key": "FEAT-1",
  "feature_name": "Audit trail",
  "title": "Add audit trail",
  "phase": "Ready",
  "events": [
    {
      "timestamp": "2026-07-09T08:00:00Z",
      "actor": "dave",
      "actor_kind": "human",
      "action": "published",
      "subject": "art-...",
      "verdict": "",
      "trust": "",
      "detail": "v1"
    }
  ]
}
```

`events` are sorted by `timestamp` **ascending**. `actor_kind` is one of
`human` / `agent` / `platform` / `""`; `trust` is one of `human` /
`agent_attested` / `platform` / `""`.

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
