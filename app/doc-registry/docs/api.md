# Doc Registry REST API Contract

This is the normative route and payload contract for
[section 6 of the Doc Registry technical specification](spec.md#6-rest-api).
Routes are registered with Huma v2 and return Huma problem details on error
unless stated otherwise. Endpoint changes must update this file and the matching
`internal/api/router*.go` registration.

## 6.1 Core Route Catalog

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

Settings and Skills:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` / `PUT` | `/settings` | Operator key/value settings |
| `GET` / `POST` | `/skills` | List/create workspace-owned Skills (`workspace_id` query/body) |
| `PUT` / `DELETE` | `/skills/{id}` | Replace/delete Skill (`workspace_id` query) |

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

## 6.2 Publish Artifact

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

## 6.3 Status Update

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

## 6.4 Conflicts

`GET /conflicts?services=` is advisory. It checks active artifacts (`draft` and
`approved`) with overlapping impacted services/apps. It never blocks publish by
itself and never changes stored lifecycle state. Each candidate and existing
artifact reference contains only `feature_id`, `version`, and `status`.

## 6.5 Settings

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

## 6.6 Skills API

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

## 6.7 CLI REST Facades

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
| `GET` | `/api/v1/meta` | Server version, recommended CLI version, configured `web_url`, and typed `capability_details` states (`available`, `unavailable`, `configuration_required`) with no secret values; `governance_chat` probes the optional Agents chat-health route and reports missing model configuration separately from service absence |
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

## 6.8 Audit Trail

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
