# Doc Registry — Technical Specification

**Version:** v1.0 | **Status:** Current | **Stack:** Go · Postgres + pgvector · Local Blob/Object Store · S3 (optional)

---

## 1. Overview

Doc Registry is the central artifact store for the AI-assisted development system. It provides versioned storage for planning artifacts (PRD, technical spec, design, implementation plan, tasks, manifest), manages artifact lifecycle, controls downstream agent access, and emits events when artifact state changes.

Registry is **passive** — stores and serves artifacts, does not trigger workflows. Downstream agents fetch artifacts on demand; the governance-ops service publishes to the registry after each run.

---

## 2. Responsibilities

- Store artifact files (PRD, spec, design, implementation plan, tasks, assumptions, risks) in local object storage or S3
- Store artifact metadata and manifest in Postgres
- Store Artifact IDE edit sessions, authoritative working draft state, revision lineage, and diff metadata for artifact editing workflows
- Store Governance Knowledge documents, raw sources, extracted text, chunk metadata, and pgvector embeddings
- Enforce artifact versioning (`vMAJOR.MINOR` scheme)
- Manage artifact status lifecycle
- Track artifact impact level (`low | medium | high`) for prioritization and review
- Conflict detection: query artifacts by impacted services
- Expose Governance Knowledge retrieval via backend API; Governance does not read raw files directly
- Expose **embedded MCP** (streamable HTTP) when enabled in settings (`mcp.enabled`) — configured in the `settings` table (Postgres), with secrets encrypted via `SETTINGS_ENCRYPTION_KEY`
- Emit events on status transitions
- Enforce access control by role
- Retention policy — cleanup of old superseded / needs-changes artifacts
- Serve coding-agent setup packages and install script for Cursor, Codex, and Claude Code

---

## 3. Storage Architecture

### 3.1 Object storage — Artifact documents

An artifact package holds **flexible documents** — any set of files an authoring tool (IDE, OpenSpec, Spec Kit, the governance-ops) produces. Each document has an open **`path`** (its identity) and a governance **`role`**. Documents are stored in S3 at:

```
{KeyPrefix}artifacts/{feature_id}/{version}/{path}
{KeyPrefix}artifacts/standalone/{artifact_id}/{version}/{path}
```

When `STORAGE_DRIVER=local`, the same object key is stored under the local object root (`BLOB_DATA_ROOT/artifacts`) and served back through the Doc Registry content proxy.

`path` is caller-provided (validated for safety: no empty, absolute, `..`, or backslash paths) and may contain subdirectories, e.g. `proposal.md`, `spec.md`, `docs/design.md`, `specs/checkout.md`.

**Roles** (the governance vocabulary the platform speaks, not filenames): `spec` (the hero / source of truth), `design`, `plan`, `verification`, `research`, `reference`, `unspecified`, and `custom:*` (framework-specific, passed through). Many documents may share a role. A role is declared by the publisher; the platform classifies a document only when its role is `unspecified`.

Access is via signed URLs (time-limited, read-only) when S3 is configured, plus the Doc Registry content proxy for browser-safe reads. S3 access is not exposed directly to agents or UI.

---

### 3.2 Database — Metadata & manifest

Storage backend is **Postgres only** (`DATABASE_DRIVER` must be set to `postgres`; the process refuses to start otherwise). Schema is authoritative via the embedded `migrations/postgres/0001_init.migration`, an idempotent file applied in full on every start (no migration-tracking table). GORM `AutoMigrate` is never used. Boolean columns use Postgres native `BOOLEAN` type.

Postgres connects via `POSTGRES_DSN`.

#### Table: `artifacts`


| Column             | Type     | Constraints     | Description                                       |
| ------------------ | -------- | --------------- | ------------------------------------------------- |
| `id`               | TEXT     | PK              | UUID v4                                           |
| `feature_id`       | TEXT     | INDEX           | Stable Feature key for feature-backed artifacts; empty for standalone quick Context Pack artifacts |
| `version`          | TEXT     | NOT NULL        | `vMAJOR.MINOR` format                             |
| `status`           | TEXT     | NOT NULL        | See status enum                                   |
| `request_type`     | TEXT     | NOT NULL        | `new_feature | change_request | bugfix | unknown` |
| `impact_level`     | TEXT     | NOT NULL        | `low | medium | high`                             |
| `artifact_phase`   | TEXT     | NOT NULL        | `phase1 | phase2`                                 |
| `artifact_completeness` | TEXT | NOT NULL        | `partial | full`                                   |
| `confidence_score` | REAL     |                 | 0.0 – 1.0                                         |
| `ambiguity_score`  | REAL     |                 | 0.0 – 1.0                                         |
| `governance_version`  | TEXT     |                 | Governance-ops service version string             |
| `created_by`       | TEXT     | NOT NULL        | `governance-ops` or user identity                  |
| `approved_by`      | TEXT     |                 | Approver identity                                 |
| `approved_at`      | DATETIME |                 | Approval timestamp                                |
| `source_kind`      | TEXT     |                 | Governance envelope — authoring tool: `cursor \| claude_code \| codex \| openspec \| spec_kit \| governance \| manual \| …` |
| `source_id`        | TEXT     |                 | Envelope — external id/uri for lineage            |
| `source_revision`  | TEXT     |                 | Envelope — source commit / external revision      |
| `authority`        | TEXT     |                 | Envelope — what the package authoritatively represents (e.g. `product_intent`) |
| `gates_profile`    | TEXT     |                 | Envelope — selected readiness profile (interpreted by later sub-projects) |
| `parent_artifact_id` | TEXT   |                 | Lineage — id of the immediately preceding artifact in the revision chain (empty for the first artifact in a chain) |
| `lineage_root_id`  | TEXT     |                 | Lineage — id of the first artifact that started this revision chain; self-referencing for chain roots |
| `created_at`       | DATETIME | NOT NULL        | Row creation timestamp                            |
| `updated_at`       | DATETIME | NOT NULL        | Last update timestamp                             |


#### Table: `artifact_services`

Normalized table for `impacted_services` and `impacted_apps` — supports conflict detection queries.


| Column        | Type | Description           |
| ------------- | ---- | --------------------- |
| `artifact_id` | TEXT | FK → `artifacts.id`   |
| `name`        | TEXT | Service or app name   |
| `kind`        | TEXT | `service | app`       |


#### Table: `artifact_files`


PK is `(artifact_id, path)`. A document is identified by its open path; `role` is a non-unique governance tag.

| Column        | Type    | Description                                                                                                   |
| ------------- | ------- | ------------------------------------------------------------------------------------------------------------- |
| `artifact_id` | TEXT    | FK → `artifacts.id` (PK part)                                                                                 |
| `path`        | TEXT    | Open document path (PK part), also the S3 object name — e.g. `spec.md`, `docs/design.md`                      |
| `role`        | TEXT    | `spec | design | plan | verification | research | reference | unspecified | custom:*` (default `unspecified`) |
| `s3_path`     | TEXT    | Full S3 object path                                                                                           |
| `size_bytes`  | INTEGER | File size                                                                                                     |


#### Table: `artifact_events`

Append-only event log for audit trail and downstream notification.


| Column        | Type     | Description                                                                                              |
| ------------- | -------- | -------------------------------------------------------------------------------------------------------- |
| `id`          | TEXT     | PK, UUID v4                                                                                              |
| `artifact_id` | TEXT     | FK → `artifacts.id`                                                                                      |
| `event_type`  | TEXT     | `artifact.published | artifact.needs_changes | artifact.superseded | feature.canonical_changed | change_request.acceptance_criteria_changed` |
| `payload`     | TEXT     | JSON — event schema per Section 8                                                                        |
| `created_at`  | DATETIME | Event timestamp                                                                                          |

#### Table: `gate_runs`

The single store for gate evaluation snapshots. One row per gate verdict,
scoped by `subject_kind` to either a work item (workboard readiness gates and
the post-build `delivery_review`) or an artifact (artifact readiness gates and
IDE-agent gate-task results). The workboard gate-run endpoints (§15) read
`change_request` subjects; the artifact readiness endpoints (§15) and the
readiness aggregate read `artifact` subjects with `executor = 'platform'`.
A submitted IDE-agent gate result is persisted as an `artifact` row whose `id`
is the dispatching `gate_tasks.id` (one result per task, last-wins upsert);
its result-only fields (`result_id`, `gate_digest`, `trust`, `evaluator`,
`findings`) ride `evidence_json`. `gate_tasks` itself stays a separate table —
it is dispatch/queue state (frozen inputs, TTL), not run history; a task is
"pending" while no `gate_runs` row with its id exists.

| Column          | Type     | Description |
| --------------- | -------- | ----------- |
| `id`            | TEXT     | PK. UUID v4 for platform runs; the `gate_tasks.id` for IDE-agent result runs |
| `subject_kind`  | TEXT     | `artifact \| change_request` |
| `subject_id`    | TEXT     | Artifact id or ChangeRequest id (serialized as `change_request_id` on the workboard wire shape) |
| `gate`          | TEXT     | Gate key, e.g. `scope_clear`, `delivery_review`, `spec_completeness` |
| `state`         | TEXT     | `pass \| warn \| fail \| needs_human_review \| not_applicable \| pending \| not_run` |
| `hint`          | TEXT     | Short human-readable next step (empty for IDE-agent result rows) |
| `executor`      | TEXT     | `platform` for platform-side evaluations (deterministic + LLM judges, readiness); the submitting executor (`ide_agent`, …) for gate-task results |
| `proposal_ref`  | TEXT     | Optional link to a draft/edit-session proposal |
| `evidence_json` | TEXT     | JSON evidence payload (contract `gate-run-v1` for workboard rows); free text allowed for artifact readiness evidence |
| `created_at`    | DATETIME | Run timestamp |

#### Table: `governance_feedback_events`

Append-only feedback signals from coding agents, delivery systems, and linked trackers. Signals may later draft proposals, but the event row remains immutable history.

| Column | Type | Description |
| --- | --- | --- |
| `id` | TEXT | PK, UUID v4 |
| `integration_id` | TEXT | Optional integration that produced the signal |
| `resource_id` | TEXT | Optional integration resource that produced the signal |
| `webhook_event_id` | TEXT | Optional recorded webhook inbox event |
| `delivery_link_id` | TEXT | Optional linked delivery object |
| `feature_id` | TEXT | Optional Work Board Feature id. For tracker / comment-drift webhook feedback, resolved from the `fixes SPECGATE-{key|id}` correlation ref alongside `change_request_id` |
| `change_request_id` | TEXT | Work Board change request id. Tracker (`delivery.tracker_status_changed`) and comment-drift feedback resolve the `fixes SPECGATE-{key|id}` ref to the work item **UUID** (not the raw key) so the signal surfaces on the work item; an unmatched ref leaves it empty |
| `artifact_id` | TEXT | Optional linked source-of-truth artifact id |
| `event_type` | TEXT | `coding_agent.blocked_ambiguity | coding_agent.completed | coding_agent.docs_updated | delivery.ci_passed | delivery.comment_scope_drift | delivery.pr_merged | ...` |
| `payload_json` | TEXT | Structured evidence / comment / repo metadata |
| `status` | TEXT | Persisted as `pending | processed | ignored` today; higher-level UI may label these as `received | accepted | rejected`, and future lifecycle values may add `triaged | proposal_drafted` |
| `reason` | TEXT | Human-readable summary |
| `created_at` | DATETIME | Event timestamp |
| `updated_at` | DATETIME | Last lifecycle transition timestamp |

#### Table: `governance_threads`

Lightweight governance-chat sidebar index. LangGraph remains the source of truth for full checkpoint/transcript state; Doc Registry stores only cheap presentation fields so the UI can load recent threads without hydrating each LangGraph thread.

| Column       | Type     | Description                                |
| ------------ | -------- | ------------------------------------------ |
| `thread_id`  | TEXT     | PK, LangGraph governance-chat thread id    |
| `title`      | TEXT     | Sidebar title                              |
| `preview`    | TEXT     | Optional short transcript preview          |
| `archived`   | BOOLEAN  | Soft-delete flag for sidebar hiding        |
| `created_at` | DATETIME | First time the summary was mirrored        |
| `updated_at` | DATETIME | Last user-visible activity / title update  |

#### Table: `workboard_lifecycle_events`

Append-only workboard audit log for workboard lifecycle transitions that are not bound to a specific artifact row. Current writes include `feature.status_changed`, `change_request.archived`, and `change_request.unarchived`.

| Column         | Type     | Description |
| -------------- | -------- | ----------- |
| `id`           | TEXT     | PK, UUID v4 |
| `entity_kind`  | TEXT     | Domain entity type (`feature`, `change_request`) |
| `entity_id`    | TEXT     | Domain entity id |
| `event_type`   | TEXT     | Lifecycle event (`feature.status_changed`, etc.) |
| `actor`        | TEXT     | Optional actor/user id |
| `payload_json` | TEXT     | JSON payload with transition metadata |
| `created_at`   | DATETIME | Event timestamp |

#### Table: `users`

Local identity records for attribution and task ownership. These rows are not
authentication sessions and do not enforce access control.

| Column | Type | Description |
| --- | --- | --- |
| `id` | UUID | PK, internal stable user id |
| `username` | TEXT | Unique normalized username (`a-z0-9_-`, 3-40 chars, starts alphanumeric) |
| `display_name` | TEXT | Human display name |
| `email` | TEXT | Optional email |
| `created_at` | DATETIME | Row creation timestamp |
| `updated_at` | DATETIME | Last update timestamp |

#### Table: `workspaces`

Local workspace records selected by CLI clients. Current behavior is attribution
and selection only; workspace isolation and RBAC are out of scope.

| Column | Type | Description |
| --- | --- | --- |
| `id` | UUID | PK, internal stable workspace id |
| `slug` | TEXT | Unique normalized workspace slug |
| `name` | TEXT | Human workspace name |
| `created_at` | DATETIME | Row creation timestamp |
| `updated_at` | DATETIME | Last update timestamp |

#### Table: `workspace_members`

Membership join table between users and workspaces. `role` is descriptive in
this slice and is not used for authorization.

| Column | Type | Description |
| --- | --- | --- |
| `workspace_id` | UUID | FK → `workspaces.id`, PK part |
| `user_id` | UUID | FK → `users.id`, PK part |
| `role` | TEXT | Descriptive role, currently `owner` for bootstrap membership |
| `created_at` | DATETIME | Row creation timestamp |


#### Table: `artifact_edit_sessions`

Server-owned working sessions for Artifact IDE. Each session is anchored to one immutable base artifact (id + version) used to detect base drift on save.


| Column               | Type     | Description                                                       |
| -------------------- | -------- | ----------------------------------------------------------------- |
| `id`                 | TEXT     | PK, `aes_<uuid>`                                                  |
| `base_artifact_id`   | TEXT     | FK → `artifacts.id`                                               |
| `base_version`       | TEXT     | Base artifact version (drift check on save → `stale_base`)        |
| `base_revision_id`   | TEXT     | Optional saved draft revision ancestor when editing a draft       |
| `state`              | TEXT     | `active | saved | discarded | stale_base | expired`              |
| `saved_revision_id`  | TEXT     | Latest saved draft revision created from this session, if any     |
| `last_diff_summary`  | TEXT     | Cached diff summary for the current working set                   |
| `requested_by`       | TEXT     | Actor that opened the session (no HTTP auth; supplied in body)    |
| `source_kind`        | TEXT     | Proposal origin (`feedback_event`, `gate`, …); empty = ordinary edit session |
| `source_id`          | TEXT     | Id of the origin (e.g. the feedback event / gate) the proposal reconciles |
| `created_at`         | DATETIME | Session creation timestamp                                        |
| `updated_at`         | DATETIME | Last session update                                               |


#### Table: `artifact_edit_session_files`

Authoritative working file state for Artifact IDE sessions. Both the base snapshot and the current working content are stored as rows so a session — and the diff/hunk ids derived from base+working content — survive a process restart.


| Column        | Type | Description                                              |
| ------------- | ---- | -------------------------------------------------------- |
| `session_id`  | TEXT | FK → `artifact_edit_sessions.id` (PK part)               |
| `file_key`    | TEXT | Artifact file key (PK part)                              |
| `role`        | TEXT | `base` (immutable snapshot) or `working` (PK part)       |
| `content`     | TEXT | File content for this role                               |


#### Table: `artifact_edit_hunk_decisions`

Append-only audit log of per-hunk apply/reject decisions (per §FR-2.3 / §FR-2.7). The latest row for a `hunk_id` wins on read (ordered `decided_at`, then `id`; sub-millisecond ties are undefined — acceptable at user-click cadence), and earlier rows remain as the audit trail. Because `hunk_id` is derived from base+working content, editing a file yields new ids and stale decisions stop matching the current diff. Decisions are **honored on save** (per §FR-2.4): a `rejected` hunk reverts to base content, so it is excluded from the saved revision diff, while `applied`/`pending` hunks are kept. Decision granularity is currently per file.


| Column        | Type     | Description                                          |
| ------------- | -------- | ---------------------------------------------------- |
| `id`          | TEXT     | PK, `aehd_<uuid>`                                    |
| `session_id`  | TEXT     | FK → `artifact_edit_sessions.id`                     |
| `hunk_id`     | TEXT     | Deterministic hunk id (`hunk_<hash>`)                |
| `file_key`    | TEXT     | Artifact file key the hunk belongs to                |
| `state`       | TEXT     | `pending | applied | rejected`                       |
| `actor`       | TEXT     | Decision actor (supplied in request body)            |
| `decided_at`  | DATETIME | Decision timestamp                                   |


#### Table: `artifact_edit_revisions`

Saved draft revisions produced by Artifact IDE. The rendered diff is snapshotted as JSON at save time so it survives even if the base artifact later moves on. Saving never overwrites an approved artifact — it only creates a draft revision row (`artifact_id` stays empty until a separate materialization step links one).


| Column                     | Type     | Description                                                      |
| -------------------------- | -------- | ---------------------------------------------------------------- |
| `revision_id`              | TEXT     | PK, `aer_<uuid>`                                                 |
| `base_artifact_id`         | TEXT     | Base artifact used to create the draft revision                  |
| `artifact_id`              | TEXT     | FK → `artifacts.id` when the draft is materialized as an artifact |
| `state`                    | TEXT     | `saved | stale_base | stale_parent`                              |
| `session_id`               | TEXT     | FK → `artifact_edit_sessions.id`                                 |
| `summary`                  | TEXT     | Human save summary                                               |
| `diff_json`                | TEXT     | Snapshotted diff payload (summary + files + unified diff)        |
| `parent_revision_id`       | TEXT     | Previous saved revision in the chain (prior save in this session, else the draft the session was opened against) |
| `lineage_root_artifact_id` | TEXT     | Artifact the edit chain descends from                            |
| `created_at`               | DATETIME | Revision creation timestamp                                      |


#### Table: `skills`

User-defined skills. **Prompt** is a single plain `TEXT` field (markdown: instructions + body together). Skills are team policy/rubric content; profiles bind a Skill to a gate via the snapshot `gate_skills` map, not per governance node. Deploy-time seed skills are starter rubrics only: the default seed creates missing defaults by name and preserves existing Skills. Operators may run `--seed-skills --seed-skills-overwrite` for an intentional starter-rubric refresh; that mode updates existing seeded names when description or prompt drift from the embedded starter set.


| Column          | Type     | Constraints         | Description                                           |
| --------------- | -------- | ------------------- | ----------------------------------------------------- |
| `id`            | TEXT     | PK                  | UUID v4                                               |
| `name`          | TEXT     | NOT NULL            | Display name                                          |
| `description`   | TEXT     | NOT NULL            | Short description                                     |
| `prompt`        | TEXT     | NOT NULL            | Markdown prompt (required)                            |
| `created_at`    | DATETIME | NOT NULL            | Row creation                                          |
| `updated_at`    | DATETIME | NOT NULL            | Last update                                           |


---

## 4. Status Lifecycle


| Status                   | Meaning                                                                                                                                    |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `draft`                  | Generated by the governance-ops service, not yet reviewed or approved                                                                      |
| `needs_changes`          | Human reviewed and requested Governance to regenerate a new version before downstream consumption                                             |
| `approved`               | Reviewed and approved by human — eligible for downstream consumption                                                                       |
| `superseded`             | Replaced by a newer version of the same feature                                                                                            |

**Transition rules:**

- `draft` → `approved`: human approval action via API
- `draft` → `needs_changes`: human request-changes action via API; artifact retained for audit but not eligible for downstream consumption
- `needs_changes` → `superseded`: a new version for the same `feature_id` is published/approved and replaces the old one
- `approved` → `superseded`: a new version is published for the same `feature_id`

Artifact IDE notes:

- Approved artifacts remain immutable; Artifact IDE save creates a new draft revision instead of mutating the base artifact in place
- Multiple child draft revisions may coexist under one approved or needs-changes base artifact; lineage and compare-token checks decide whether a session save is still valid
- Draft revisions created by Artifact IDE remain subject to the same review and approval governance as governance-ops-published drafts

---

## 5. Versioning Scheme

Artifacts use `vMAJOR.MINOR` versioning:

- **Major bump:** scope or contract changes significantly (new feature scope, breaking API change)
- **Minor bump:** iterative updates within the same feature scope (spec refinement, task updates)

When a new version is published for an existing `feature_id`, the previous `approved` version automatically transitions to `superseded`.

Version is assigned by the governance-ops service at publish time. Registry validates the format (`^v\d+\.\d+$`) and rejects malformed versions.

---

## 6. REST API

### 6.1 Endpoints


| Method + Path                                | Description                                                                                                                                                                                                                               | Auth                              |
| -------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `POST /artifacts`                            | Publish a new artifact version. Body carries `documents: [{ path, role, content (base64) \| ref }]` plus the governance envelope (`source_kind/source_id/source_revision/authority/gates_profile`). A fixed-key `files`/`file_refs` map is also accepted — each key is translated to `{path, role}` server-side; `documents` takes precedence when both are present. | Governance service account           |
| `GET /artifacts/{id}`                        | Fetch artifact by ID. The response also carries a derived, read-only `expected_gates` array (the `enabled_gates` parsed from `gates_profile_snapshot_json`) so the UI can show which gates a readiness run will execute before any run exists; omitted when no snapshot is persisted. | All authenticated                 |
| `DELETE /artifacts/{id}`                     | Delete an artifact and its stored files (best-effort S3 cleanup). Returns 204.                                                                                                                                                           | Open (internal)                   |
| `GET /artifacts`                             | List artifacts (filter by `feature_id`, `service`, `status`)                                                                                                                                                                              | All authenticated                 |
| `PATCH /artifacts/{id}/status`               | Update status (approve, request changes, supersede)                                                                                                                                                                                       | Approver or Governance               |
| `GET /artifacts/{id}/files`                  | List the artifact's documents — `{ items: [{ path, role, size_bytes }] }` (path-sorted); optional `?role=` filter. Lets readiness enumerate role-tagged documents over arbitrary spec formats. | All authenticated                 |
| `GET /artifacts/{id}/files/{key}`            | Get signed S3 URL for an artifact document. Document paths contain `/`, so the document is selected by the **`?path=`** query param (the `{key}` segment selects by fixed file key); response may also include inline valid UTF-8 `content` as a browser-safe fallback when Doc Registry can read an object up to 1 MiB directly. (`signed_url` is retained for agent container-side reads; the browser uses `content` or the `/content` proxy below.)                                                                                                                        | All authenticated                 |
| `GET /artifacts/{id}/files/{key}/content`    | Stream an artifact file's content through the API (server-side S3 read), the browser's fallback for files larger than the 1 MiB inline limit — no presigned URL. Cacheable (`ETag` = id+key, `max-age`, `nosniff`); 304 on match.                                                                                                                        | All authenticated                 |
| `POST /features/{id}/attachments`            | Pin a feature-scoped reference attachment (`kind` ∈ `link`/`file`/`image`). `link` requires `url`; `file`/`image` require a `governance_file_id`. `audience` ∈ `gate`/`coding_agent`/`both` defaults to `gate` (reaching the coding agent is opt-in). Returns the attachment DTO.                                                                          | Open (internal)                   |
| `GET /features/{id}/attachments`             | List a feature's reference attachments, newest first. Scoped to `feature_id` so they survive spec re-publishes.                                                                                                          | Open (internal)                   |
| `DELETE /attachments/{id}`                   | Delete a feature reference attachment by id (404 if unknown).                                                                                                                                                             | Open (internal)                   |
| `POST /governance/files/upload`                 | Upload a file's bytes **through the API** (server-side write to BlobStore or S3 per `STORAGE_DRIVER`) — creates the row, stores the object, commits to ready, returns the DTO. Body = file bytes; `Content-Type` = mime; `X-File-Name` (URL-encoded) = filename. The **browser** uses this so it never PUTs to (or sees credentials for) the object store. The upload mirror of `/content`. | Open (internal)                   |
| `POST /governance/files/presign`                | Presign S3 upload for an internal governance file; writes a pending row. Used by **agents** (container-side, where S3 is reachable); the browser uses `/upload` instead. This is blob plumbing for explicit governed pins and compatibility flows, not a user-facing file manager. | Open (internal)                   |
| `POST /governance/files/{id}/commit`            | Mark a presigned file as ready (agent presign flow). No presigned object-store URL is returned (`get_url` is empty).                                                                                                      | Open (internal)                   |
| `GET /governance/files`                         | List ready files ordered by last_used_at desc, with optional `q` substring.                                                                                                                                               | Open (internal)                   |
| `GET /governance/files/{id}/content`            | Stream a ready file's bytes through the API (server-side S3 read) with the stored `Content-Type` under `Content-Security-Policy: sandbox`, so **no** client (browser **or** agent) needs a presigned object-store URL. Cacheable (`ETag` = id, `max-age`, `nosniff`); 304 on match; 404 for unknown/pending. Both the browser and the agent read governance-file content here; `get_url` is no longer returned by any governance-file endpoint. | Open (internal)                   |
| `POST /governance/files/{id}/touch`             | Refresh last_used_at (keeps a file warm against the TTL purger). No presigned object-store URL is returned (`get_url` is empty).                                                                                          | Open (internal)                   |
| `DELETE /governance/files/{id}`                 | Delete the row and (best-effort) the underlying S3 object.                                                                                                                                                                | Open (internal)                   |
| `GET /governance/threads`                       | List lightweight governance-chat thread summaries ordered by `updated_at DESC`; supports `limit` and `offset`.                                                     | Open (internal)                   |
| `PUT /governance/threads/{thread_id}`           | Upsert a lightweight governance-chat thread summary (`title`, `preview`, optional `updated_at`) for fast sidebar loading.                                          | Open (internal)                   |
| `DELETE /governance/threads/{thread_id}`        | Archive a lightweight governance-chat thread summary so it no longer appears in default lists.                                                                      | Open (internal)                   |
| `GET /conflicts?services=`                   | Check conflict overlap for a list of services; optional `candidate_feature_id`, `candidate_version`, `candidate_status` to populate `feature_a` in the response                                                                           | Governance service account           |
| `GET /events`                                | Poll event log (filter by `event_type`, `artifact_id`, `after` timestamp); `artifact_id` scopes events to a single artifact for audit-log surfaces                                                                                                                                                                              | All authenticated                 |
| `GET /repos/file?project=&path=&ref=`        | Read one repository file through the integration-credentialed GitLab provider. `project` is a GitLab integration resource `external_key`; `ref` empty uses the resource default ref. Response `{content, found}`: a missing file → `200 {found:false}`, an unknown project → `404`, an upstream GitLab/transport error → `502`. The integration token is resolved server-side and never leaves Doc Registry. Source is GitLab integrations × project resources (`ListGitLabRepoConfigs`), not external `gitlab` MCP rows | Open (internal)                   |
| `GET /mcp/info`                              | MCP status: listen address, `restart_required`, tool catalog (read from settings; `repo_*` advertised iff a GitLab integration repo is connected)                                                                                                                                   | Open (internal network; see §6.5) |
| `GET /mcp/api-key`                           | Effective MCP access token (`api_key`), streamable `path`, and `env_overridden` — backs the Settings → MCP connect snippets                                                                                                               | Open (internal; do not expose)    |
| `POST /mcp/api-key/rotate`                   | Rotate `mcp.api_key` (mint + persist a new token; invalidates the old one). When `MCP_API_KEY` env is set it still wins until removed from the env                                                                                         | Open (internal)                   |
| `GET /settings`                              | All configuration keys (MCP/GitLab; secrets masked `*`**). Header `Authorization: Bearer` matching `mcp.api_key` → returns fully decrypted version (trusted MCP client)                                                                            | Open (internal)                   |
| `PUT /settings`                              | Batch update of key-value pairs; sensitive values encrypted with AES-256-GCM                                                                                                                                                              | Open (internal)                   |
| `GET /integrations`                          | List native workflow integrations (`items[]`); stores provider-level connection metadata outside generic settings/MCP rows                                                                                                                | Open (internal)                   |
| `POST /integrations`                         | Create a native workflow integration (`provider`, `name`, optional `base_url`, `status`, `config_json`)                                                                                                                                  | Open (internal)                   |
| `GET /integrations/{id}`                     | Read one native workflow integration. The DTO exposes presence flags only (`has_api_token` / `has_oauth_token` / `has_webhook_secret`); secret/token ciphertext is never returned                                                          | Open (internal)                   |
| `PUT /integrations/{id}`                     | Sparse-update mutable integration metadata. Conflicting unique keys return 409                                                                                                                                                           | Open (internal)                   |
| `DELETE /integrations/{id}`                  | Delete a native workflow integration; cascades credentials, resources, webhook events, delivery links, and governance feedback. Returns 204.                                                                                                 | Open (internal)                   |
| `GET /integrations/{id}/resources`           | List external resources linked to one integration, e.g. GitLab project/repo mappings                                                                                                                                                      | Open (internal)                   |
| `GET /integrations/{id}/repos`               | List the repos/projects the integration's token can access (GitLab/GitHub only) for the connect-time repo picker. Query `search` (name filter) and `limit` (default 50, max 100). Resolves the stored PAT/OAuth token server-side and queries GitLab `GET /projects?membership=true` / GitHub `GET /user/repos`; returns `{external_id, external_key, display_name, default_ref}` items mapping straight onto a resource. Linear → 400 (no repos); an upstream provider failure → 502 | Open (internal)                   |
| `GET /integrations/{id}/linear/teams`        | List the Linear teams the integration's PAT/OAuth grant can access for the connect/manage resource picker. Resolves the stored outbound credential server-side and queries Linear GraphQL `teams { nodes { id key name } }`; returns `{external_id, external_key, display_name}`. Non-Linear integrations → 400; upstream provider failure → 502. | Open (internal)                   |
| `GET /integrations/{id}/linear/projects`     | List projects for one Linear team, for the optional second picker after selecting a team. Query `team_id` is required. Resolves the stored outbound credential server-side and queries Linear GraphQL `team(id) { projects { nodes { id name slugId } } }`; returns `{external_id, external_key, display_name}`. Non-Linear integrations or blank `team_id` → 400; upstream provider failure → 502. | Open (internal)                   |
| `POST /integrations/{id}/resources`          | Create a linked external resource (`resource_type`, `external_key`, optional `external_id`, `default_ref`, `config_json`). For GitLab/GitHub project resources and Linear team resources, if the integration already has outbound auth (PAT/OAuth), the server also auto-creates the provider webhook, stores a per-resource secret, and stamps `config_json` with `webhook_url`, `provider_webhook_id`, and `webhook_status=connected`. GitLab is created with **both** a `whsec_` signing token and a legacy secret token; the server stores whichever GitLab kept (`signing_token_present` in the response → signing token, else the secret token) so GitLab < 19.0 still verifies. Linear creates a team-scoped webhook via `webhookCreate` with `resourceTypes=[Issue,Comment]` and a generated secret. If webhook provisioning fails, the resource is **rolled back** (no orphan). If no outbound auth is configured yet, the resource is created without webhook provisioning. Conflicting unique key returns 409 | Open (internal)                   |
| `POST /integrations/{id}/resources/{resource_id}/reprovision-webhook` | (Re)register the provider webhook for an **existing** resource — for resources created before auto-provisioning existed, ones where provisioning was skipped (no outbound auth at the time), or to re-register after the public webhook base URL changed. Best-effort deletes any prior `provider_webhook_id` first (no duplicate hook), then provisions and stores the new secret + `config_json.webhook_status=connected` (clearing any prior `webhook_last_error`). Unlike create, the resource is **never deleted on failure**: the provider error is persisted to `config_json.webhook_status=error` + `webhook_last_error` and returned (502). Requires outbound auth + a public registry base URL. | Open (internal)                   |
| `DELETE /integrations/{id}/resources/{resource_id}` | Delete a linked external resource. For GitLab/GitHub/Linear resources with a managed webhook (`config_json.provider_webhook_id`), Doc Registry first deletes the provider webhook using the integration's outbound credential, then deletes the local row. This path is **strict**: upstream delete failure returns 502 and leaves the local resource untouched. Upstream `404/410/not found` is treated as already-removed and the local delete still proceeds. | Open (internal)                   |
| `GET /integrations/{id}/webhook-events`      | List recorded webhook inbox events for one integration; optional `resource_id`, `status`, `limit` filters                                                                                                                                 | Open (internal)                   |
| `POST /integrations/{id}/webhook-events`     | Record a webhook inbox event. Provider defaults from the integration when omitted; status defaults to `pending`. Provider-specific signature verification endpoints can layer on top of this inbox.                                      | Open (internal)                   |
| `POST /integrations/{id}/resources/{resource_id}/gitlab/webhook` | Preferred GitLab project webhook receiver. Authenticates **synchronously** — a `whsec_` **signing token** (GitLab 19.0+) → Standard Webhooks `webhook-signature` HMAC + timestamp recency, otherwise a **secret token** fallback (GitLab < 19.0) → verbatim `X-Gitlab-Token` compare — then **enqueues** the delivery for async processing (a worker normalizes, project-matches the resource's `external_id`/`external_key`, and commits). See "Async webhook processing" below. Returns 401 on auth failure, 413 on oversized body, 200 (accepted) otherwise. | Verified (per-resource) |
| `POST /integrations/{id}/resources/{resource_id}/github/webhook` | Preferred GitHub repository webhook receiver. Authenticates `X-Hub-Signature-256` against the **resource's** stored secret **synchronously**, then **enqueues** for async processing (worker repo-matches + commits). Returns 401 on signature mismatch, 413 on oversized body, 200 (accepted) otherwise. | Signature-verified (per-resource) |
| `POST /integrations/{id}/resources/{resource_id}/linear/webhook` | Resource-scoped Linear tracker webhook receiver. Authenticates `Linear-Signature` (bare-hex HMAC-SHA256) against the **resource's** stored per-resource secret (provisioned via `webhookCreate`); falls back to the global `LINEAR_WEBHOOK_SECRET` env secret when the resource has no stored secret (manual setup). Processes `Issue` and `Comment` events through the same tracker-status / scope-drift pipeline as the integration-level route. Requires a `resource_type=team` resource. Returns 401 on signature mismatch, 400 on non-team resource, 413 on oversized body, 200 on success or ignored. | Signature-verified (per-resource, fallback global) |
| `POST /integrations/{id}/gitlab/webhook`     | Per-integration GitLab webhook receiver. Verifies the **Standard Webhooks** signing token (the per-integration `whsec_` token): HMAC-SHA256 over `{webhook-id}.{webhook-timestamp}.{body}` matched against any `v1,<base64>` in `webhook-signature`, plus a timestamp-recency check; rejected with 401 when no signing token is configured, the timestamp is stale, or the signature mismatches. Caps body at 1 MiB, and ingests the event in a single DB transaction. Replays dedup by `X-Gitlab-Event-UUID`; when that header is absent, dedups by `sha256:` of the raw payload. Both `processed` and `failed` prior outcomes count as duplicates so retries cannot double-fire governance feedback. Also handles `Issue Hook` events (`object_kind=issue`) as a GitLab-as-tracker signal: emits `delivery.tracker_status_changed` (status from a scoped `workflow::` label — `done`→completed, `doing`→started — else the issue open/closed state) correlated by the persisted `tracker_links` row first, falling back to the `fixes SPECGATE-{key}` footer; no resource match required (tracker augments, never gates). Returns 401 on signature mismatch, 413 on oversized body, 200 on success or ignored | Signing-token verified (per-integration) |
| `POST /integrations/{id}/github/webhook`     | GitHub peer of the GitLab receiver. Verifies `X-Hub-Signature-256` (HMAC-SHA256 of the raw body) against the **per-integration** webhook secret (Doc-Registry-generated); rejected with 401 when no secret is configured. Processes three event types: `pull_request` delivery events (same shared pipeline as GitLab → `delivery.pr_*`), `issue_comment` scope-drift comments (→ `delivery.comment_scope_drift`), `workflow_run` CI completion events (→ `delivery.ci_passed` on `conclusion=success`; others silently ignored). All other event types are ignored. Replays dedup by `X-GitHub-Delivery` or `sha256:` of the raw payload. Returns 401 on signature mismatch, 413 on oversized body, 200 on success or ignored | Signature-verified (per-integration) |
| `POST /integrations/{id}/linear/webhook`     | Linear (tracker) webhook receiver. Verifies `Linear-Signature` (bare-hex HMAC-SHA256 of the raw body, no `sha256=` prefix) against the configured `LINEAR_WEBHOOK_SECRET` env secret; rejected with 401 when that secret is unset. Processes `Issue` events for tracker status and `Comment` events for correlated scope drift (others ignored). Trackers are optional: it does not require a registered resource or a matched work item. Correlates the work item by the persisted `tracker_links` row (matched on the issue's `data.id`/`identifier`) first, falling back to the `fixes SPECGATE-{key|id}|fixes SPECGATE-{key|id}` footer from `data.description` or comment body; carries the Linear `data.state.type` raw, emits `delivery.tracker_status_changed` **only on a real state transition** (same state ⇒ ignored `tracker_status_unchanged`) and `delivery.comment_scope_drift` for correlated comments. A top-level `action: "remove"` (issue deleted) emits a terminal `tracker_state="removed"` and marks the link removed. Replays dedup by `webhookId` or `sha256:` of the raw payload. Returns 401 on signature mismatch, 413 on oversized body, 200 on success or ignored | Signature-verified (env secret)   |
| `PUT /integrations/{id}/api-token`           | Set or rotate the provider API token for outbound calls (Linear GraphQL / GitLab REST / GitHub REST + hosted MCP). Body `{api_token: string}`; blank values rejected with 400; providers without a tracker adapter rejected with 400 — `providerSupportsAPIToken` derives from the tracker-adapter registry, so registering the GitHub adapter makes `github` a token-bearing provider. Stored AES-256-GCM encrypted (`api_token_encrypted`); write-only — never returned. `GET /integrations/{id}` exposes only the derived `has_api_token` presence flag. Setting a PAT flips `auth_method=pat` and clears any stored OAuth grant so only one outbound auth method stays active. Returns 204 | Open (internal)                   |
| `GET /integrations/{id}/webhook-secret`      | Manual fallback: reveal the per-integration inbound-webhook secret (GitLab/GitHub only; Linear → 400). GitHub generates one on first access (get-or-create) so it can be copied into GitHub's webhook *Secret* field; GitLab returns the stored signing token (empty until pasted — we never generate GitLab's). Returns `{secret, has_webhook_secret}` (plaintext; same internal-trust model as `GET /mcp/api-key`). Preferred product flow is resource-scoped auto-provisioned webhooks, not this integration-level manual setup. | Open (internal)                   |
| `PUT /integrations/{id}/webhook-secret`      | Manual fallback: store a user-provided integration-level webhook secret — GitLab's pasted `whsec_` **signing token** (format-validated: `whsec_` + base64 32-byte key) or a custom GitHub secret. Body `{secret}`; blank or non-self-serve provider (Linear) → 400. Stored AES-256-GCM encrypted (`webhook_secret_encrypted`). Preferred product flow is resource-scoped auto-provisioned webhooks. | Open (internal)                   |
| `POST /integrations/{id}/webhook-secret/rotate` | Manual fallback: regenerate the **GitHub** integration-level webhook secret (returns the new value). GitLab → 400 (rotate the signing token in GitLab and re-paste). Preferred product flow is resource-scoped auto-provisioned webhooks. | Open (internal)                   |
| `POST /integrations/oauth/begin`             | Begin OAuth for a **not-yet-created** integration (create-on-callback). Body `{provider, name, base_url?, config_json?, redirect_target?}`. The pending spec + PKCE verifier are stored on a short-lived `integration_oauth_states` row with an empty `integration_id`; **no integration row is created**, so a cancelled/failed connect leaves no orphan. Returns `{authorize_url}` (built via `golang.org/x/oauth2` with an S256 PKCE challenge). | Open (internal)                   |
| `POST /integrations/{id}/oauth/authorize`    | Begin OAuth **re-authorization for an existing integration** (e.g. re-auth an expired grant, or switch a PAT integration to OAuth). Body `{redirect_target?}` (app-relative path). Looks up the provider's OAuth app credentials from env (`{GITHUB,GITLAB,LINEAR}_OAUTH_CLIENT_ID`/`_SECRET`), persists a state row carrying the existing `integration_id` + PKCE verifier, and returns `{authorize_url}`. | Open (internal)                   |
| `GET /integrations/oauth-callback`           | OAuth callback ingress. Validates `state`, exchanges `code` (replaying the PKCE verifier), fetches the account identity, then **creates the integration** from the state's pending spec when `integration_id` is empty (create-on-callback) or **updates the grant on the existing integration** otherwise — consuming the state row in the same transaction for the create path. Clears any stored PAT, then redirects back to the saved app-relative target (default `/?settings=integrations`). The `redirect_uri` origin is derived from the request (scheme + `X-Forwarded-Host`/`Host`), overridable via `OAUTH_PUBLIC_CALLBACK_BASE_URL`, and resolved identically in the authorize and callback legs so the provider's exact-match check passes. | Provider callback ingress         |
| `POST /integrations/{id}/oauth/disconnect`   | Clear stored OAuth grant material for an integration while preserving the integration row and linked resources. Returns 204. | Open (internal)                   |
| `GET /governance/feedback-events`                | List planning-agent feedback events newest first; optional filters `status`, `change_request_id`, `artifact_id`, and `limit`. Persisted statuses are `pending/processed/ignored`; higher-level UI may label them as received/accepted/rejected.                              | Open (internal)                   |
| `POST /governance/feedback-events/{id}/status`  | Set a feedback event's triage status (resolve/dismiss). Body `{status}`. Returns the updated event.                                                                                                                                      | Open (internal)                   |
| `GET /governance-profiles`                   | List SpecGate governance profiles (built-in + imported) for the read-only profile catalog. Response `{ items: [...] }`; each item carries `namespace`, `key`, `full_key`, `version`, `display_name`, `change_type`, `required_roles/topics/evidence`, `enabled_gates`, `renderer_key`, `digest`, `source` (`builtin`/`import`), and the **effective** `approval_policy` / `evidence_policy` (default applied so the catalog never shows an empty cell). Read-only; profiles are imported via the MCP `specgate_import_profiles` tool. | Open (internal)                   |
| `GET /skills`                                | List of skills from the `skills` table — **JSON envelope: §6.7**                                                                                                                                                                          | Open (internal)                   |
| `POST /skills`                               | Create skill (`name`, `prompt` required; `description`) — **body: §6.7**                                                                                                                                                                  | Open (internal)                   |
| `PUT /skills/{id}`                           | Replace entire skill — **body: §6.7**                                                                                                                                                                                                     | Open (internal)                   |
| `DELETE /skills/{id}`                        | Delete skill — **response: §6.7**                                                                                                                                                                                                         | Open (internal)                   |
| `POST /artifact-edit/sessions`               | Create an Artifact IDE editing session from a base artifact. Optional `source_kind`/`source_id` tag it as a reconciliation **proposal** linked to its origin. Optional `working_files` (`[{key, content}]`) seed resolved working content atomically — a client re-applying edits onto a moved base creates the session already-resolved in one request, with no create-then-write window to orphan a session | Open (internal)                   |
| `GET /artifact-edit/proposals`               | Review queue: artifact-update proposals (sourced edit sessions still `active`) awaiting a human verdict. Approve = save the session (draft revision); reject = discard it                                                                  | Open (internal)                   |
| `GET /artifact-edit/sessions/{id}`           | Read Artifact IDE session metadata                                                                                                                                                                                                      | Open (internal)                   |
| `DELETE /artifact-edit/sessions/{id}`        | Discard an Artifact IDE session                                                                                                                                                                                                         | Open (internal)                   |
| `GET /artifact-edit/sessions/{id}/files`     | List current working files for an Artifact IDE session                                                                                                                                                                                  | Open (internal)                   |
| `GET /artifact-edit/sessions/{id}/files/{key}` | Read one current working file from an Artifact IDE session                                                                                                                                                                            | Open (internal)                   |
| `POST /artifact-edit/sessions/{id}/patch`    | Apply a structured patch to a working file in an Artifact IDE session                                                                                                                                                                   | Open (internal)                   |
| `PUT /artifact-edit/sessions/{id}/files/{key}` | Replace a working file in an Artifact IDE session                                                                                                                                                                                     | Open (internal)                   |
| `GET /artifact-edit/sessions/{id}/diff`      | Return the authoritative server diff for an active Artifact IDE session                                                                                                                                                                 | Open (internal)                   |
| `POST /artifact-edit/sessions/{id}/save`     | Save the session as a draft revision and close the session                                                                                                                                                                             | Open (internal)                   |
| `GET /artifact-revisions/{revision_id}`      | Read saved draft revision metadata                                                                                                                                                                                                      | Open (internal)                   |
| `GET /artifact-revisions/{revision_id}/diff` | Return the authoritative server diff for a saved draft revision                                                                                                                                                                         | Open (internal)                   |
| `GET /artifacts/{id}/revisions`              | List saved draft revisions for a base artifact                                                                                                                                                                                          | Open (internal)                   |
| `GET /api/v1/artifacts/{artifact_id}/gate-preview` | Preview expected gate tasks from stored profile snapshot | Open (internal) |
| `POST /api/v1/artifacts/{artifact_id}/gate-tasks` | Dispatch `ide_agent` gate tasks for the artifact's enabled gates (one per enabled LLM gate). Resolves Skill rubrics, computes digests, sets a TTL. Idempotent per `(gate_key, gate_digest)`. Returns `{artifact_id, created_task_ids, skipped_gate_keys}`. 201 Created. | Open (internal) |

`POST /artifacts` rejects duplicate `(feature_id, version)` publishes with `409 Conflict`. This preserves `feature_id + version` as the immutable artifact identity for feature-backed artifacts and prevents database uniqueness errors from surfacing as `500` responses to governance-ops callers. Standalone quick Context Pack artifacts use an empty `feature_id` and a timestamped version, and do not create Feature rows. A feature-backed publish that supplies a stale `base_version` (no longer the feature's latest) is likewise rejected with `409 Conflict` before any write, surfacing a clean stale-base signal instead of a raw uniqueness conflict.


---

### 6.1A Feature reference attachments (`artifact_attachments` table)

Reference attachments are links, files, and screenshots a product team pins to a **feature** to support quality-gate review and the coding-agent handoff. They are scoped to `feature_id` (not a single artifact version) so a bug screenshot or reference link survives spec re-publishes — unlike `artifact_files`, which are immutable per version. File/image bytes reuse `governance_files` (presign → PUT → commit); the `artifact_attachments` row holds only `governance_file_id` (or a raw `url` for `kind=link`).

Each row carries an `audience` flag that controls downstream consumption: `gate` (quality-gate reviewer only), `coding_agent` (handoff context only), or `both`. The default is `gate` — reaching the coding agent is a deliberate opt-in, never automatic, so the handoff context is not bloated with every attachment. The agent context-pack includes `coding_agent`/`both` rows; the quality-gate judge includes `gate`/`both` rows. Audience is an explicit user choice (optionally pre-filled by an LLM suggestion over the title/note — never keyword rules over that user text).

Attachment creation is explicit. Files shared with an IDE/coding-agent session
do not become `artifact_attachments` rows unless a client deliberately creates
one through this endpoint. Session-local files are implementation context; files
that change product intent, scope, acceptance criteria, rollout, or design
contract must enter the artifact-edit proposal flow instead of being stored as
supplemental references.

**Keying.** `feature_id` is the feature **key** (the same value artifacts publish as `feature_id` and the UI reads as `plan.featureId`), not the feature UUID. Every reader (UI panel, agent gate, coding-agent context-pack) must use that key, or `ListByFeature` returns nothing.

**Validation + retention.** A `link` row's `url` must be an absolute `http(s)` URL (other schemes — `javascript:`, `data:` — are rejected at create so a stored link can't become a script-injection vector). A `file`/`image` row's `governance_file_id` must reference an existing **ready** `governance_files` row; the TTL purger (`DeleteStaleReady`) treats `artifact_attachments.governance_file_id` as a pin, so an attached upload survives the governance-file retention window like an artifact file does. The content proxy serves uploads under `Content-Security-Policy: sandbox` so an untrusted `text/html` / `image/svg+xml` upload cannot script the registry origin if opened top-level (it still renders inside the UI's `allow-scripts` iframe).

---

### 6.2 `POST /artifacts` — Request body

```json
{
  "feature_id": "checkout-loyalty-points",
  "request_type": "change_request",
  "impact_level": "high",
  "artifact_phase": "phase1",
  "artifact_completeness": "partial",
  "version": "v0.3",
  "confidence_score": 0.78,
  "ambiguity_score": 0.22,
  "governance_version": "governance",
  "base_version": "v0.2",
  "impacted_services": ["order-service", "loyalty-service"],
  "impacted_apps": ["web-checkout"],
  "design_refs": [
    {
      "type": "figma",
      "url": "https://figma.com/file/...",
      "frame_id": "123:456",
      "snapshot_url": "https://registry/.../design-snapshot.png",
      "captured_at": "2026-04-16T10:00:00Z",
      "note": "Checkout confirmation flow"
    }
  ],
  "files": {
    "prd": "<base64 content or presigned upload URL>",
    "spec": "...",
    "design": "...",
    "implementation_plan": "...",
    "tasks_fe": "...",
    "tasks_be": "...",
    "tasks_qa": "...",
    "assumptions": "...",
    "risks": "..."
  },
  "file_refs": {
    "spec": "governance-file-01J..."
  }
}
```

`files` remains the inline/base64 publish path for small governance artifacts. `file_refs` is optional and maps artifact file keys to already-committed governance file IDs so larger artifacts can reuse the governance-files presign + commit flow instead of embedding bytes in JSON. When both maps contain the same file key, Doc Registry rejects the request as ambiguous.

`design_refs` is optional. Registry stores the reference in manifest/artifact metadata so reviewers and downstream agents can trace the source of a design decision, but Registry does not call Figma itself or validate mockup content.

`impact_level` is required metadata set by the governance-ops service at publish time. Registry stores this value in the artifact record and manifest; it does not infer it from the number of services/apps.

`base_version` is an optional optimistic lock. When omitted, no check is performed. When provided, it names the version this publish was derived from: if it is no longer the feature's latest version, Doc Registry rejects the publish with `409 Conflict` (`stale base version`) instead of writing — so the caller re-fetches the latest and rebases rather than silently overwriting concurrent work. This is ergonomics over the existing `(feature_id, version)` unique index, which already rejects a second publish that derives the same version string; `base_version` turns that raw uniqueness conflict into a clean stale-base signal that names the current latest.

`artifact_phase` and `artifact_completeness` are planning-stage metadata:

- `artifact_phase=phase1`, `artifact_completeness=partial`: partial draft awaiting review
- `artifact_phase=phase2`, `artifact_completeness=full`: complete draft with implementation plan

#### Governance policy fields (additive — `impact_declaration`, `requested_governance_level`)

These fields are optional and backward-compatible: existing callers that omit
them are unaffected.

```json
{
  "request_type": "bugfix",
  "impact_level": "low",
  "requested_governance_level": "light",
  "impact_declaration": {
    "protected_domains": [],
    "affected_systems": ["checkout-api"],
    "data_or_schema_change": "no",
    "external_contract_change": "no",
    "irreversible_or_complex_rollback": "no",
    "broad_blast_radius": "no",
    "expected_paths": ["app/checkout/**"]
  }
}
```

**`requested_governance_level`** (optional; `light` | `standard` | `enhanced`): the caller's preferred governance level. Policy resolution may override this upward. The resolved level is stored in `governance_level` inside `gates_profile_snapshot_json` and may differ.

**`impact_declaration`** (optional object): caller-supplied impact signals used by the policy engine to determine the governance level. Missing object or any missing individual tri-state field normalizes to `"unknown"` at resolution time.

| Field | Type | Values | Description |
| --- | --- | --- | --- |
| `protected_domains` | array of string | e.g. `["payment"]` | Named protected domains this change touches. |
| `protected_domains_status` | tri-state | `yes` \| `no` \| `unknown` | Whether the change affects a protected domain. **`"unknown"` is the sole trigger for `criticalUnknown()`** — forces `governance_level` to `enhanced` regardless of other fields. |
| `data_or_schema_change` | tri-state | `yes` \| `no` \| `unknown` | Change modifies a database schema, stored data format, or migration. |
| `external_contract_change` | tri-state | `yes` \| `no` \| `unknown` | Change modifies a public or cross-service API contract. |
| `irreversible_or_complex_rollback` | tri-state | `yes` \| `no` \| `unknown` | Rollback requires multi-step coordination or is not fully reversible. |
| `broad_blast_radius` | tri-state | `yes` \| `no` \| `unknown` | Change affects a large surface area across multiple services or teams. |
| `affected_systems` | array of string | — | Informational. Not used in policy resolution. |
| `expected_paths` | array of string | glob patterns | Informational. Not used in policy resolution. |

---

### 6.2B Resolved governance snapshot (`gates_profile_snapshot_json` v1)

Artifacts published or updated with governance fields carry a `gates_profile_snapshot_json` blob using `snapshot_schema_version: "specgate.policy/v1"`. This is an immutable record of the governance decision at publish time. Consumers must not re-derive governance level from live policy unless they explicitly discard and refresh the snapshot.

Policy v1 snapshots add the following fields on top of the pre-existing
execution projection (`required_roles`, `required_topics`, `required_evidence`,
`enabled_gates`, `renderer_key`, `approval_policy`, `evidence_policy`,
`gate_skills`, `digest`):

```json
{
  "snapshot_schema_version": "specgate.policy/v1",
  "work_type": "bugfix",
  "risk_level": "high",
  "requested_governance_level": "light",
  "governance_level": "enhanced",
  "reason_codes": ["high_impact"],
  "required_roles": ["spec", "verification"],
  "required_topics": ["outcomes", "scope", "acceptance_criteria", "verification"],
  "required_evidence": ["tests", "rollout_defined"],
  "enabled_gates": ["spec_completeness", "acceptance_criteria_verifiable"],
  "renderer_key": "default_context_pack",
  "approval_policy": "human_required",
  "evidence_policy": "corroborated_required",
  "gate_skills": {},
  "policy_lineage": [{"key": "builtin/enhanced", "version": "1"}],
  "digest": "sha256:..."
}
```

| Field | Type | Description |
| --- | --- | --- |
| `snapshot_schema_version` | string | Always `"specgate.policy/v1"` for policy v1 snapshots. Snapshots without this field are parsed as the plain `ResolvedProfile` shape. |
| `work_type` | string | Normalized work type (`bugfix` \| `change_request` \| `new_feature`). Supersedes `change_type` in new snapshots. |
| `risk_level` | string | Resolved risk level (`low` \| `medium` \| `high`). Derived from `impact_level` and `impact_declaration`. |
| `requested_governance_level` | string \| null | Caller's preferred level from the publish request; may have been overridden upward by policy. Null when not supplied. |
| `governance_level` | string | The policy-resolved governance level (`light` \| `standard` \| `enhanced`). This is the enforced level. |
| `reason_codes` | array of string | Machine-readable codes explaining the resolved governance level. Valid values: `low_risk_bugfix`, `high_impact`, `complex_rollback`, `broad_blast_radius`, `protected_domain`, `unknown_protected_domain`, `default_standard`. |
| `policy_lineage` | array of `{key, version}` | Ordered list of built-in policy entries applied during resolution. Allows consumers to detect stale snapshots when policy has changed. |

**Conformance fixtures** for the policy resolution engine (5 canonical test cases) live at `docs/conformance/governance-policy-v1/resolution-cases.json`.

---

### 6.3 `PATCH /artifacts/{id}/status` — Request body

```json
{
  "status": "approved",
  "approved_by": "tech-lead@company.com",
  "review_rating": 3,
  "note": "Minor edits applied to API section",
  "manifest": "{...updated manifest.json...}",
  "actor_kind": "human"
}
```

`review_rating`: 1 = unusable, 2 = major edits, 3 = minor edits, 4 = ready to use.

`manifest` is optional. When present, Doc Registry overwrites the artifact's stored `manifest.json` object after the status transition so governance-promoted drafts do not keep stale `status:"draft"` or empty approval metadata in the artifact bundle.

`actor_kind` is optional (`"human"` | `"agent"`); defaults to `"human"` when absent. This is a **cooperative surface check** — `actor_kind` is client-asserted and there is no server-side identity enforcement. The field is used to enforce the artifact's profile `approval_policy`:

- `approval_policy: human_required` + `actor_kind: agent` → `403 Forbidden` (`ErrApprovalRequiresHuman`)
- `approval_policy: self_approve` or `auto` → any `actor_kind` is accepted
- Empty snapshot → defaults to `human_required` (safe default)

The `specgate_publish` auto-approve path uses `actor_kind: agent` with an `auto` profile — the guard allows it.

Request changes uses the same endpoint, with `status: "needs_changes"` and `note` required so the governance-ops service knows which sections to regenerate. Artifacts in `needs_changes` are excluded from approved-artifact reads and are not consumed by downstream agents.

---

### 6.4 `GET /conflicts?services=` — Query params and Response

**Query params:**


| Param                  | Required | Description                                                                                              |
| ---------------------- | -------- | -------------------------------------------------------------------------------------------------------- |
| `services`             | ✓        | Repeated or comma-separated list of impacted service names                                               |
| `candidate_feature_id` |          | Feature ID of the candidate being checked for conflict — used to populate `feature_a.feature_id` in the response |
| `candidate_version`    |          | Version of the candidate — populates `feature_a.version`                                                 |
| `candidate_status`     |          | Status of the candidate (default: `draft`) — populates `feature_a.status`                                |


```json
{
  "conflict_state": "blocking_conflict",
  "conflicts": [
    {
      "conflict_id": "uuid",
      "type": "blocking_conflict",
      "feature_a": {
        "feature_id": "checkout-loyalty-points",
        "version": "v0.3",
        "status": "draft",
        "manifest_url": "https://registry/.../manifest-a.json"
      },
      "feature_b": {
        "feature_id": "pricing-v2",
        "version": "v1.2",
        "status": "approved",
        "manifest_url": "https://registry/.../manifest-b.json"
      },
      "overlapping_services": ["order-service"],
      "overlapping_modules": [],
      "resolution_options": [
        "merge_into_single_feature",
        "prioritize_feature_a",
        "prioritize_feature_b",
        "manual_scope_split"
      ]
    }
  ]
}
```

### 6.2A Artifact IDE session and revision endpoints

Artifact IDE uses server-owned session state.

Implementation status:

- Route + schema surface is registered end-to-end in HTTP router and OpenAPI.
- Handlers create server-owned edit sessions, maintain authoritative working file state, return session/revision diffs, and save draft revisions.
- Session state, base/working file snapshots, per-hunk decisions, and saved revisions are persisted in the durable tables from §3.2 (`artifact_edit_sessions`, `artifact_edit_session_files`, `artifact_edit_hunk_decisions`, `artifact_edit_revisions`), so sessions survive a restart and support multi-process deployment. Handlers require the store to be wired; routes return `501` when it is absent.

Phase-1 behavioral rules:

- `POST /artifact-edit/sessions` creates an `active` session from one base artifact. If an older base artifact references a missing object-store file, the session still opens with the readable files instead of failing the whole edit flow.
- `POST /artifact-edit/sessions/{id}/save` creates a durable saved draft revision (with `parent_revision_id` + `lineage_root_artifact_id` lineage) and closes the session on success. It never overwrites the approved artifact — the revision is a draft (`artifact_id` empty) until a separate materialization step. Rejected hunks are excluded from the saved revision (see §FR-2.4 below). **Reconciliation materialization (§1):** when the session has `source_kind=feedback_event` or `source_kind=coding_agent_update`, save also creates a new `draft` artifact from the effective working files (`source_kind="reconciliation"`, `source_id=revision_id`), sets `Revision.artifact_id` to the new artifact's ID, and returns `materialized_artifact_id` in the response body. This is a **best-effort side-effect**: a materialization failure logs an error and returns an empty `materialized_artifact_id` without rolling back the revision. Non-proposal sessions (`source_kind` absent or other values) never materialize and always return `materialized_artifact_id=""`. The materialized artifact is a `draft` in the normal review flow — a human must approve it before it can become canonical. Materialized artifacts are always created with `artifact_completeness=full` so the Publish button is immediately active.
- If base ancestry changed since session creation, save returns a stale outcome such as `stale_base` or `stale_parent` instead of silently rebasing
- `POST /artifact-edit/sessions/{id}/patch` also supports hunk decision updates by sending `hunk_id` + `hunk_state` (`pending|applied|rejected`) without changing file content. Decisions are persisted append-only with the deciding actor (`decided_by` in the request body, since Doc Registry has no HTTP auth) and a timestamp (per §FR-2.7); the latest decision per `hunk_id` wins on read. Editing a file changes its content-derived `hunk_id`, so stale decisions stop matching the diff rather than being destructively cleared.
- A session created with `source_kind`/`source_id` is a reconciliation **proposal** (an artifact update suggested from a signal — a feedback event, a failing gate). `GET /artifact-edit/proposals` is the review queue of such sessions still `active`. The human verdict reuses the existing session verbs: **approve = save** (creates a draft revision; never a canonical mutation, per §FR-2.4 / §FR-5.6), **reject = discard**. The proposal's *content* is drafted upstream (the governance-ops); Doc Registry owns the queue and the verdict, not the drafting.
- The verdict on a `source_kind=feedback_event` proposal **reconciles the originating signal**: approve marks the linked `governance_feedback_events` row `processed`, reject marks it `ignored` (with a verdict reason). This closes the feedback→proposal→verdict loop so a signal that has been acted on leaves the pending feedback queue. The reconciliation is a best-effort side-effect across the artifact-edit and integrations stores (not a shared transaction): the proposal verdict has already committed and is never undone if the signal annotation fails. On approve of a `delivery.pr_merged`-sourced proposal, the linked Feature advances `planned → active` (best-effort via `h.WorkBoard.UpdateFeature`, gated on `h.WorkBoard != nil` and the Feature still being `planned`); any other proposal only reconciles the signal and produces a draft revision. ChangeRequest **status** stays derived (no persisted execution state per the data-model decision) and is never moved on approve.
- Diff returned by `GET /artifact-edit/sessions/{id}/diff` and `GET /artifact-revisions/{revision_id}/diff` is authoritative server output, not client-derived state
- Diff file rows include deterministic hunk ids (`hunk_<hash>`) derived from session id, file key, and base/current content, and remain stable across repeated reads while session content is unchanged
- Each hunk row in the diff also carries its own content (`file_key`, `base_text`, `working_text`) alongside `id` and `state`, so a reviewer can render and decide each hunk without re-deriving it from the file-level `unified_diff`. Hunk granularity is currently one hunk per modified file

---

### 6.5 Settings and MCP

MCP/GitLab configuration is stored in the `**settings**` table (Postgres), key-value, with an `encrypted` flag for secrets. **Required** a single env variable for encryption: `SETTINGS_ENCRYPTION_KEY` (32 bytes, 64-character hex). The process **refuses to start** if the key is missing.

**Initialization:** configure MCP/GitLab via `PUT /settings` or the UI. The two keys `mcp.enabled` and `mcp.api_key` additionally accept values from env (`MCP_ENABLED`, `MCP_API_KEY`) — env wins over settings when set (including set empty, used to disable MCP). The remaining keys are read only from the settings service.

**Hot reload:** `SettingsService` cache TTL is 30s — changes to GitLab/budget/API key via `PUT /settings` take effect within ~30s (the MCP handler dynamically rebuilds when `ConfigHash` changes).

**GitLab repos are sourced from GitLab integrations** (provider=`gitlab` integrations × their `resource_type="project"` resources; see §6.6), not from settings keys. There are no `gitlab.*` keys in `settings` and no outbound external-MCP-server registry. Repo credentials (base URL + PAT) and `project_id` / `default_ref` live in `integrations` / `integration_resources`.

**Restart required:** `GET /mcp/info` returns `restart_required: true` when `mcp.enabled` in settings differs from the value at process startup (operational hint; the MCP transport shares the HTTP server with REST).

### 6.6 Embedded MCP and `GET /mcp/info`

**Streamable HTTP transport** is mounted on the **same** HTTP server as the REST API, at the fixed path `**/mcp/stream`** (e.g. `http://localhost:8080/mcp/stream` when `HTTP_ADDR=:8080`). Clients (agents) set `MCP_SERVER_URL` to this full URL with Bearer `mcp.api_key`. The `mcp.addr` key in settings can still be used for UI/metadata but **does not** open a separate listener in the current implementation. The MCP handler reads configuration from `SettingsService` (rebuilds internally when the hash changes).

**MCP authentication:** `Authorization: Bearer` matching the effective value of `mcp.api_key` (env `MCP_API_KEY` wins over settings when set). `X-SpecGate-Run-ID` may be sent for budget tracking.

**MCP access token (generated, not env-hardcoded).** When neither `MCP_API_KEY` env nor a `mcp.api_key` setting is configured, the server **auto-generates** a random `mcp.api_key` into settings on startup (idempotent — `mcp.EnsureAPIKey`), so the streamable endpoint is always gated without manual setup. `MCP_API_KEY` env still overrides when set. Two internal-only endpoints back the Settings → MCP connect UI: `GET /mcp/api-key` returns the effective token + streamable `path` + `env_overridden`; `POST /mcp/api-key/rotate` mints and persists a new token (when `MCP_API_KEY` env is set it still wins as the effective token until removed from the env). Both share the `/mcp/info` posture — **no auth; internal/network-boundary only, do not expose publicly**. The governance-ops service reads the token from settings using the trusted `X-SpecGate-Internal-Agent: governance` read (not Bearer), so reading the token never depends on the token and rotation cannot lock the service out.

**Deployment.** `/mcp/stream` shares the HTTP port with the unauthenticated internal endpoints (`/settings`, `/mcp/info`, `/mcp/api-key`). To expose the Bearer-gated stream to coding agents outside the trusted network, front it with a reverse proxy that exposes **only** `/mcp/stream` — never the internal endpoints.

`**GET /mcp/info`:** reads live from settings; adds `restart_required` when `mcp.enabled` differs from boot time; always includes `tools[]` (catalog). The `repo_*` tools appear in the catalog iff at least one GitLab **integration** repo config is connected (`ListGitLabRepoConfigs` non-empty), mirroring runtime registration. No GitLab connectivity field is returned — only the inbound governance tools are advertised.

`repo_*` MCP tools accept an optional `repo` (GitLab `project_id`) to dispatch between multiple connected integration repos. With a single repo, `repo` may be omitted; with multiple repos configured, the caller must pass the correct `project_id`. Tool dispatch errors return a JSON tool-result containing `{error, available}` (not wrapped in the error path) so the LLM can parse + retry on the next turn. `repo_context_pack` is a first-pass tool with caching in the MCP server instance: input `query`, optional `repo/ref/paths/globs/max_results`; output includes README excerpt, search matches, likely files, `cached`, and `cache_ttl` so the governance-ops can reduce repeated `repo_search` + `repo_read_file` round trips. GitLab file reads reuse content returned by the file metadata endpoint when available, avoiding a second `/raw` request on cold small-file reads. Metadata and small raw blobs are cached under the immutable GitLab revision id (`last_commit_id`) plus `blob_id`; mutable refs such as branches are revalidated via metadata before a cached body is reused so moved refs do not serve old repo content.

Artifact MCP tools include `search_artifacts`, `artifact_read_bundle`, `artifact_create`, and `draft_artifact_update`. `artifact_read_bundle(artifact_id, files?, max_chars?)` returns artifact metadata and selected markdown content. `artifact_create` publishes a new artifact bundle with the same payload as `POST /artifacts`. `draft_artifact_update` opens a draft-only sourced `artifact_edit_session` (`source_kind=coding_agent_update`) seeded with full replacement markdown for changed existing file keys; it never auto-saves or mutates the approved artifact. Governance-ops reads artifacts by explicit artifact id from work items, Context Packs, or search results.

All embedded MCP resources use the **`specgate://`** scheme. The handoff renderer compiles the Context Pack from the artifact's **role-tagged documents**, driven by the artifact's **profile snapshot** (`renderer_key`):

- `specgate://context-pack/{change_request_id}` (and the lane variants `…/fe`, `…/be`) — CR-scoped handoff for a work item (the governance-era path). Assembled on-read; lane (`fe`/`be`) omits the other discipline's task breakdown.
- `specgate://context-pack/artifact/{artifact_id}` — **artifact-scoped** handoff for an IDE-published feature-artifact (no change-request). Full pack, **no lanes** — the developer pulls the whole approved spec into their open repo.
- `specgate://skills`, `specgate://skills/{id}` — the skills resources.

The pack payload is a JSON object with the following top-level keys: `state` (`"assembled"` | `"empty"`), `markdown` (rendered handoff document), `source_artifact_id` (ID of the artifact the pack was assembled from, empty when none), **`canonical_files`** (a `{path → markdown-content}` map of the source artifact's files, populated when `state="assembled"` and the source artifact is readable, empty map otherwise), and **`knowledge_provenance`** (CR-scoped only — see below). `canonical_files` enables IDE agents to apply approved spec content directly to open repository files without a separate artifact-read round-trip — each key is the artifact file path, each value is the full file content as a UTF-8 string. For quick-route work, `context_pack_artifact_id` is the source and its stored `implementation-plan.md` is served without nesting; normalized acceptance-criteria rows refresh the pack's Acceptance Criteria section, with the stored `acceptance_criteria_json` as fallback.

**`knowledge_provenance`** (CR-scoped packs only; absent from artifact-scoped packs): an array of provenance rows for knowledge documents linked to the feature or change request. Each row: `document_id`, `title`, `version`, `document_type`, `authority_level`, `is_latest` (bool), `freshness` (`"current"` when `is_latest=true`; `"stale"` when a newer version exists), `knowledge_store_uri` (`specgate://knowledge/{document_id}`). The array is always present and never null — it is `[]` when no documents are linked. Rows are sorted by authority priority (`source_of_truth` → `high` → `reference` → `low`), then alphabetically by title within each tier. Multiple versions of the same document are deduplicated: the `is_latest=true` row wins; when no version is latest the most recent `created_at` wins. The matching `### Knowledge References` table section is added to the rendered markdown between the Execution Brief block and the spec content sections. Knowledge lookup errors are non-fatal: the array is `[]` and a `knowledge_provenance_unavailable` warning entry is added to the `warnings` array.

The pack is a **derived view assembled on-read**, never a stored artifact. Its instruction header is **CLI-first**: it tells coding agents to read the pack before editing, treat the approved spec as the contract, and use the SpecGate CLI (`specgate work ...`, `specgate delivery report ...`, `specgate delivery review ...`) for the handoff loop instead of raw MCP tool names. **Role-based assembly:** the renderer groups the artifact's documents by `role` and emits sections in order — **Spec → Design → Implementation Plan → Verification → Research → Reference** (each concatenating that role's documents in stable path order); `unspecified`/`custom:*` documents appear under **Additional Documents** so arbitrary-framework docs are never dropped. Files named `glossary.md`, `vocabulary.md`, `domain-vocabulary.md`, `domain-language.md`, `ubiquitous-language.md`, `CONTEXT.md`, or carried by custom roles such as `custom:glossary` are surfaced in a derived **Domain Vocabulary** section and are not duplicated in Reference or Additional Documents. **Profile-driven:** the artifact's `gates_profile_snapshot_json` selects the renderer via `renderer_key` (`default_context_pack` = the role-based renderer) and annotates the pack with the change-type's required roles. The snapshot may also carry `gate_skills` (a `{gate_key → skill_name}` map; built-ins bind a rubric Skill to each enabled gate plus `delivery_review`): the agents readiness/delivery judges resolve each bound Skill's prompt and inject it as a "Team Policy" rubric, so a gate is judged against the team's Skill in addition to its built-in prompt (gate-consumes-Skills). The handoff renderer also surfaces those same bound Skills as an **Applicable Skills** section: the snapshot's `gate_skills` values (Skill names) are deduped and resolved against the registered Skills into pointer rows (`- {name} — {description}: specgate://skills/{id}`) so the coding agent pulls each Skill's current version via its `specgate://skills/{id}` resource (the pack carries the pointer, not the inlined prompt). The section is omitted when the profile binds no Skills or none resolve. `gate_skills` rides the snapshot only — it is not returned by `GET /governance-profiles`. CR-scoped packs fall back to fixed-key assembly when the artifact has no role-tagged files (quick-lane `implementation_plan` served verbatim). **Re-handoff delta** ("Outstanding Review Feedback") and **"Unresolved Quality Gates"** folds are preserved. Resource reads are side-effect free.

IDE plugin routes are served from the same process: `/plugins/install.sh`, the package inventory (`/plugins/package.json`), the Codex marketplace manifest (`/plugins/.agents/plugins/marketplace.json`), native plugin manifests (`/plugins/.codex-plugin/plugin.json`, `/plugins/.claude-plugin/...`, `/plugins/.cursor-plugin/...`), the landing-derived plugin logo (`/plugins/assets/logo.svg`), the focused `/plugins/skills/{name}/SKILL.md` routes, shared hook files under `/plugins/hooks/...`, and Cursor rules under `/plugins/rules/...`. The `/plugins/package.json` inventory declares shared plugin metadata, the focused Skills, retired Skill cleanup names, and exact file allowlist for the `/plugins/*` handler; unlisted package paths return 404. Native Codex, Claude Code, Cursor, and marketplace JSON metadata is generated from that inventory by `make generate-plugins`; `make check-plugins` fails if generated metadata or embedded package copies drift. `install.sh` is the per-developer onboarding entry point: run with no `--agent` it presents an interactive IDE picker (reading the choice from `/dev/tty` so it works under `curl … | sh`, including comma-separated multi-select such as `1,2`), installs or locates the `specgate` CLI, then delegates actual IDE writes to `specgate --server <registry> plugins install`. The CLI writes global user-level IDE Skills/hooks/rules by default (`~/.cursor`, `~/.codex`, `~/.claude`) so setup is install-once across repositories; `--project-local` is the explicit opt-in for repo-local files. Codex native plugin behavior is owned by the Codex marketplace + `.codex-plugin` manifest, focused Skills, and default `hooks/hooks.json`; `specgate plugins install` writes those files into the personal plugin package at `~/.codex/plugins/specgate`, merges the generated `/plugins/.agents/plugins/personal-marketplace.json` entry into the user's personal Codex marketplace, and enables `specgate@personal` in Codex config without requiring Python on the user's machine. Claude Code behavior is owned by `.claude-plugin`, focused Skills, and `hooks/hooks-claude.json`; the CLI writes that native plugin package to `~/.claude/skills/specgate`. Cursor behavior is owned by `.cursor-plugin`, focused Skills, `rules/`, and `hooks/hooks-cursor.json`; the CLI writes standalone Cursor rules and focused Skills because Cursor plugin filesystem install paths are marketplace-managed. Retired fallback rule files (`AGENTS.specgate.md`, `CLAUDE.specgate.md`) are not written or served; project-local reinstalls remove those bridge files and retired Skill directories when present. Installer targets are normalized and validated before writes, fetched files are written atomically through temporary files, and retired plugin-package skill directories are pruned without deleting current package content before a successful fetch. `specgate plugins doctor` checks installed Codex, Claude Code, and Cursor files after installation against the latest connected `/plugins/package.json`; missing required files fail the command, while stale installed versions or stale Codex plugin cache entries warn with the reinstall/restart action. The focused installer skills are `using-specgate`, `setting-up-specgate-project`, `checking-spec-readiness`, `shaping-work`, `picking-up-work`, `implementing-work`, and `completing-delivery`; served Skill guidance documents invocation modes: `using-specgate` is the everyday router, `setting-up-specgate-project` is explicit setup for repository onboarding or refreshed plugin context, and lifecycle skills stay focused on one phase and are normally reached through the router. The session hook injects only the short router. Templates render the live Doc Registry base URL so one installer can target local, staging, or production environments. `--agent cursor|codex|claude|all` is the non-interactive form, comma-separated subsets such as `--agent cursor,codex` are also supported, `--skip-cli` refreshes IDE assets without reinstalling the CLI, and `--dry-run` previews the delegated install command. Human output is step-oriented (`Choose IDE setup` → `Install SpecGate CLI` → `Write IDE files`) so local operators can see progress without reading raw shell output. Agent-package files are embedded into the doc-registry binary at build time. In Docker dev mode, the canonical root `plugins/` directory is mounted at `watch/plugins`; Air polls that mount, runs `scripts/sync-embedded-plugins.sh`, regenerates embedded native metadata from the copied inventory, and rebuilds automatically before `/plugins/...` serves changed assets. Production Docker builds sync the root `plugins/` tree into the embedded package immediately before `go build`; other non-watch binaries still require a rebuild/redeploy because the assets are compiled into the binary.

**CLI installer route.** `GET /cli/install.sh` returns an instance-aware shell script that installs the SpecGate CLI for the server's current version and configures it to connect to this instance. The script is rendered at request time using the request origin (`requestBaseURL`) and `buildinfo.Version`; it calls `specgate config set server <origin>` after install and never reads or embeds MCP credentials (`/mcp/api-key` or `MCP_API_KEY`). The installer logic is sourced from `scripts/install-cli.sh` in the repo and served as a self-contained script by `/cli/install.sh` rather than delegating to a separate hosted wrapper asset, so update/install does not depend on a second `install-cli.sh` download path. Human output is step-oriented (`Resolve version` → `Prepare install target` → `Download package`/`Build binary` → `Install binary`) and `specgate update --json --json-progress` can emit compact progress-event lines before the final JSON envelope for wrappers that need streaming status. When `--install-dir` is omitted, the installer prefers the current `specgate` binary's directory when writable, then `~/.local/bin`, and only falls back to `/usr/local/bin`. The instance-aware route still downloads the versioned release archives and checksums from GitHub releases during installation.

Embedded MCP tools also expose **work discovery + review readbacks** for the IDE-native handoff loop:

- `get_governance_status()` — no input required. Returns an aggregate governance snapshot in two queries (all CRs + all stale warnings). Output: `{ summary, counts: { intake, draft, review, ready, handoff, total }, attention: [{ change_request_id, key, title, phase, issues }] }`. `attention` lists only CRs with at least one stale warning; empty slice means no issues. Intended as the first call at session start to orient the developer before picking up a work item.
- `resolve_work_item(provider, issue_key?, issue_url?)` resolves a handed-off tracker issue (Linear / GitLab / GitHub) to `{change_request_id, change_request_key, feature_id, title, phase, context_pack_uri, issue_key, issue_url, lane}` by joining the provider's integration rows to persisted `tracker_links`; lane-specific issues return the lane-scoped Context Pack URI (`specgate://context-pack/{cr}/fe` or `.../be`). Either `issue_key` or `issue_url` is required.
- `list_work_items(ready?, handed_off?, work_type?, workspace_id?, mine?, limit?)` lists ready / handed-off work items with `{change_request_id, change_request_key, feature_id, title, phase, context_pack_uri, work_type}`. Default filter is "ready or handed off"; `workspace_id` narrows results to locally attributed work items in one workspace and is a selection filter, not authorization; `mine` is reserved for identity-aware filtering and currently returns validation error when true.
- `read_delivery_review(change_request_id)` reads the latest persisted `delivery_review` GateRun and returns `{found, verdict, hint, confidence, reviewed_at, per_criterion, checks, outstanding_md}`. `found=false` means no review has run yet. On `fail`, `outstanding_md` mirrors the same unmet/unclear criteria + failing checks that the re-handoff Context Pack folds under **"Outstanding Review Feedback"**.
- `read_clarification(change_request_id, since?)` reads terminal human outcomes for `coding_agent.blocked_ambiguity` feedback and returns `{found, clarifications:[{feedback_event_id, question_ref, question_md, answer_md, status, answered_at}]}`. `status` uses the canonical feedback vocabulary (`accepted` or `rejected`), `answered_at` is the reconciled feedback `updated_at`, and `since` is an optional RFC3339 lower bound on `answered_at`. Pending blocked-ambiguity reports are intentionally omitted so agents do not treat unresolved ambiguity as an answer.

Embedded MCP tools also expose `report_implementation_feedback` — write a coding-agent feedback signal into `governance_feedback_events`. The tool validates `change_request_id`, `event_type`, `severity`, `summary`, `evidence[]`, `suggested_correction`, and optional `dedupe_key`, then creates or returns a deduplicated feedback event in `received` state. It does not draft or apply artifact changes. On `coding_agent.completed` it also accepts two **optional, backward-compatible** completion-evidence fields that ride `payload_json` (no migration): `checks[]` (`{name: tests|types|lint|build, status: pass|fail|skipped, detail?, source?}`) and `criteria[]` (`{criterion_id? (correlates to `acceptance_criteria.id`), text?, claim: satisfied|partial|not_done, evidence?}`) — the inputs the delivery reviewer (`delivery_review` gate) verifies each acceptance criterion against. Agents that omit them still succeed; the review degrades to judging against `summary`.

The `source` field on `checks[]` and `evidence[]` items is the **evidence-provenance marker** (Slice B): `"agent"` (self-reported via MCP, default when absent) | `"webhook"` | `"ci"` (independently-corroborated from the git/CI webhook ingestion path). This field is **stamped by the ingestion path**, not trusted from agent input — the MCP handler leaves `source` empty (agent-reported); a webhook handler sets it to `"ci"` or `"webhook"`. The delivery-review gate reads the artifact's `evidence_policy` from its profile snapshot; when `evidence_policy == "corroborated_required"` and no corroborated evidence is present for the CR (no `delivery.pr_merged` event), a `pass` verdict is clamped to `needs_human_review`.

Embedded MCP tools also expose `draft_artifact_update` — open a draft-only artifact-update proposal for a coding agent. The tool validates `artifact_id`, `summary`, `files`, and optional `change_request_id`, `requested_by`, and `dedupe_key`; it creates or returns a deduplicated active proposal session and seeds only changed existing file keys into the working set. It never auto-approves, auto-saves, or directly mutates the approved artifact.

**IDE publish tools (SpecGate).** Two MCP tools support IDE-native spec publishing. Both are registered when `WorkBoard` is wired (i.e. when Work Board is enabled). Governance is unchanged: publish always lands a `draft` that a human must approve before the artifact becomes canonical.

- **`specgate_publish`** — publish a spec package to SpecGate by stable `feature_key`. Input:

  | Field | Type | Required | Description |
  | --- | --- | --- | --- |
  | `feature_key` | string | yes | Stable product capability key; SpecGate creates or references the Feature by this key |
  | `feature_name` | string | no | Display name used only when creating a new Feature |
  | `base_version` | string | no | Optimistic lock: the latest version the agent fetched before drafting. When provided, SpecGate derives the new version from this base and rejects a stale base (≠ current latest) with a `version_conflict` error naming the current latest, so the agent re-fetches and rebases instead of silently overwriting. Omit for the auto-bump-from-latest default. |
  | `documents` | `[{path, role?, content}]` | yes | One or more documents; `content` is raw UTF-8 Markdown/text, not base64; `path` is the open document identity; `role` is the governance role (`spec`, `design`, `plan`, …). CLI package files may use local-only `source_file` or `file_url` fields instead; the CLI reads those files and sends raw `content` before this API/tool boundary. |
  | `source_kind` | string | no | Authoring tool: `cursor`, `claude_code`, `codex`, `openspec`, etc. |
  | `source_revision` | string | no | Source commit or external revision |
  | `source_id` | string | no | External id or URI for provenance |
  | `gates_profile` | string | no | Selected readiness profile key (must exist in the SpecGate registry when provided) |
  | `authority` | string | no | What the package authoritatively represents (default: `product_intent`) |
  | `impact_level` | string | no | `low`, `medium`, or `high` (default: `medium`) |
  | `request_type` | string | no | `new_feature`, `change_request`, or `bugfix` (auto-inferred from prior artifact history when omitted) |

  Behavior: `UpsertFeatureByKey` creates or returns the Feature; the new version is derived from the feature's latest artifact (`NextVersion` auto-bumps `vMAJOR.MINOR`; when `base_version` is supplied, `ResolveNextVersion` bumps from that base and rejects a stale base); lineage fields (`parent_artifact_id`, `lineage_root_id`) are set from the prior artifact so the revision chain is preserved; the requested `gates_profile` is resolved against the SpecGate profile registry (or, when omitted, defaults deterministically as `bugfix -> bug_fix`, else `impact_level=high -> high_impact_feature`, else `generic_change`); the resolved profile key/version/digest/snapshot are stored on the artifact; the artifact is created with `status=draft`, requiring human review before it can be approved or become canonical.

  Output: `{ artifact_id, feature_key, version, status, review_url, missing_roles, readiness_hint }` where `review_url` points to the human review page (`{APP_BASE_URL}/artifacts/{id}`). `missing_roles[]` and `readiness_hint` are a **deterministic, non-blocking required-roles pre-flight**: the resolved profile's `required_roles` minus the roles present in the published `documents[]` (computed in-process, zero extra calls). `missing_roles` is empty and `readiness_hint` reads `ready: all required roles present` when every required role has a document; otherwise it names the missing roles. The draft lands regardless — this is a hint for the IDE, not a gate.

- **`specgate_check_readiness`** — run the readiness gates for an artifact and return covered-vs-missing topics + gate verdict; check in-IDE before handoff. Input: `{ artifact_id }`. Registered **only when `AGENTS_BASE_URL` is set** — the doc-registry tool delegates the LLM gate compute to the agents service (`POST /artifacts/{id}/run-readiness`) over an internal, un-authed edge with an extended (120s) timeout. Output: `{ artifact_id, evaluations_posted?, readiness_runs[], aggregate }`, where `readiness_runs[]` is the agents response (each `{ gate, state, hint?, evidence_json?, judge_model?, created_at? }`) and `aggregate` is derived over the latest run per gate with precedence `fail > needs_human_review > warn > pass > not_run` (`not_applicable` counts as pass-equivalent; `pending` does not contribute). The edge is **soft**: agents unreachable / non-2xx → a clear `readiness unavailable: …` tool error (never panics, never affects other tools).

- **`run_llm_gates`** — run all LLM quality gates for a change request's lead artifact and post verdicts to Doc Registry. Input: `{ change_request_id }`. Registered **only when `AGENTS_BASE_URL` is set**. Delegates to the agents service (`POST /workboard/change-requests/{id}/run-llm-gates`). Returns the raw agents response (gate verdicts + evaluations count). Follow with `specgate_check_readiness` or `read_delivery_review` to read the persisted result. The edge is soft: agents unreachable → a clear `run-llm-gates unavailable: …` tool error. This is a durable CLI/MCP/agents trigger; the UI surfaces persisted gate state and advisory prompts until direct run controls have an explicit review owner.

- **`trigger_delivery_review`** — trigger the delivery review for a change request. Input: `{ change_request_id }`. Registered **only when `AGENTS_BASE_URL` is set**. Delegates to the agents service (`POST /workboard/change-requests/{id}/review-delivery`), which judges the latest `coding_agent.completed` feedback against the CR's acceptance criteria and persists a `delivery_review` gate run. When the platform model/provider is unavailable, agents derive the verdict from the coding agent's per-AC claims and checks instead of returning no verdict. Returns `{ verdict, criteria_verdicts?, outstanding_md?, reason? }`. Follow with `read_delivery_review` to read the full persisted verdict. The edge is soft: agents unreachable → a clear `review-delivery unavailable: …` tool error. This remains a CLI/MCP/agents-owned transition until the UI has a direct review action with clear confirmation and durable outcome messaging.

- **`specgate_whoami`** — health and identity check. No input. Returns `{ ok, service, tools, resources, version }`. Use this to confirm the IDE is connected to the correct SpecGate instance before publishing.
- **`specgate_list_profiles`** — list built-in and imported active SpecGate governance profiles available to IDE plugins.
- **`specgate_import_profiles`** — import immutable team governance profiles into SpecGate. Imported profiles are versioned registry objects and are referenced later by stable key on `specgate_publish`; publish does not accept inline custom profile blobs.

**IDE connect setup.** Point the IDE's MCP client at the SpecGate streamable endpoint:

1. Fetch the access token: `GET /mcp/api-key` (internal-only endpoint) returns `{ api_key, path, env_overridden }`.
2. Configure the IDE MCP client with `url = {registry-origin}/mcp/stream` and `Authorization: Bearer <api_key>`.
3. Verify with `specgate_whoami` — a successful response with `ok: true` confirms the connection.
4. Publish with `specgate_publish` by `feature_key`; the response `review_url` links to the draft for human approval.

Feedback ingestion deduplicates by `provider + external_id + event_type + change_request_id` when provider event ids are available. Coding-agent reports should prefer `run_id + event_type + change_request_id` and otherwise fall back to a stable payload hash.

**No external-MCP-server registry.** Doc Registry has no outbound external-MCP-server registry; the inbound MCP endpoint (`/mcp/info`, `/mcp/stream`, `/mcp/api-key`) exposes Doc Registry's own governance tools (`search_knowledge`, `repo_read_file`, readiness, …) to IDE coding agents. Doc Registry builds the `repo_*` provider map **solely** from connected GitLab **integrations** (provider=`gitlab` with a recoverable `api_token_encrypted`) × their `resource_type="project"` resources — `external_key`→`project_id`, `base_url`→API URL (normalized to `.../api/v4`), `default_ref`→ref via `ListGitLabRepoConfigs`. This is the unified source that also backs the tracker. The MCP catalog rebuilds when the integrations hash changes (`RepoConfigHash()`). **Token scope:** the integration's stored `api_token` authenticates these reads — `repo_read_file` / `repo_list_files` need only `read_repository` scope, but `repo_search` / `repo_context_pack` call GitLab's search API and require a fine-grained PAT with `read_api` (search) scope, else GitLab returns `403 insufficient_granular_scope`.

`**GET /settings` / `PUT /settings`:** map string → string; `mcp.api_key`, `openai.api_key`, `google.api_key`, `anthropic.api_key`, and `openrouter.api_key` mask `***` on GET when the caller is not trusted. With `Authorization: Bearer <mcp.api_key>` or internal governance-ops header `X-SpecGate-Internal-Agent: governance`, GET returns decrypted secrets for trusted service consumers. PUT sends `***` to preserve an existing secret. No `repos` field remains in the response (GitLab repos are sourced from GitLab integrations; see §6.6). Remaining model keys are the single app model pair `governance.model_provider` / `governance.model` plus `governance.default_thinking_level`; the old governance/node-tier keys (`governance.model*`, `governance.model_mini*`, `governance.model_main*`, `governance.model_system*`) are not valid settings. Other keys include `mcp.enabled`, `mcp.addr`, `mcp.api_key`, `mcp.budget_max_repo_calls`, `mcp.budget_max_bytes_returned`, `speech_to_text.provider`, `speech_to_text.model`, `openai.api_key`, `google.api_key`, `anthropic.api_key`, `openrouter.api_key`, `governance.auto_feature_summary`, and `governance.auto_archive_on_delivery_pass`. `governance.auto_feature_summary` (`"true"`/`"false"`, default `"true"`) controls whether the governance-ops service auto-regenerates a Feature's Overview narrative when its canonical artifact changes. `governance.auto_archive_on_delivery_pass` (`"true"`/`"false"`, default `"false"`) controls whether a future newly persisted passing `delivery_review` gate run archives the ChangeRequest with actor `specgate-auto-archive` and reason `delivery review passed`; it does not delete work, artifacts, evidence, or chat threads. Integration OAuth app credentials are sourced from environment config, not settings (`{GITHUB,GITLAB,LINEAR}_OAUTH_CLIENT_ID`/`_SECRET`); the public callback origin is derived from the request by default, with `OAUTH_PUBLIC_CALLBACK_BASE_URL` as an optional override for reverse-proxy / prod setups.

`speech_to_text.provider` stores the configured transcription provider and accepts `openai` or `google`; `speech_to_text.model` accepts `gpt-4o-transcribe` for OpenAI or `chirp_3` for Google. Anthropic is not accepted for speech-to-text until Anthropic exposes a public speech-to-text API/model.

Policy / operational settings (surfaced in the UI "General" tab; defaults mirror the in-code constants they replace): `governance.gate_confidence_threshold` (`0.7`, float 0–1) and `governance.lifecycle_confidence_threshold` (`0.7`, float 0–1) are the confidence floors below which the LLM quality-gate / lifecycle-suggestion judges downgrade to `needs_human_review` — the agent reads these. `governance.auto_archive_on_delivery_pass` (`false`, boolean string) archives a ChangeRequest only when a future persisted delivery review passes. `governance.feature_freshness_sla_days` (`7`, int 1–3650) and `governance.artifact_stale_days` (`5`, int 1–3650) are day-count thresholds the UI reads (feature freshness-SLA escalation; stale-artifact attention flag). `governance.publish_retry_attempts` (`3`, int 1–10), `governance.publish_retry_base_seconds` (`0.5`, float >0 ≤600), and `governance.registry_timeout_seconds` (`30`, float >0 ≤600) tune the agent's Doc Registry write path. `governancefiles.ttl_days` (`90`, int 1–3650) is the registry-side governance-file retention window the in-process purger reads on every sweep (see §"Governance Files Retention"). None are sensitive; invalid / out-of-range values are rejected by `PUT /settings`.

**CORS / deployment:** the internal-only model from §7 is preserved.

### 6.7 Skills API (`skills` table)

The governance-ops service and the UI read skill definitions over REST. `**name`** is the stable key clients use to pick a skill (for example `spec-review`, `prd-review`, `review-impl`, `acceptance-criteria`, `task-breakdown`, and `rollout-risk`); `**prompt**` is the markdown content injected into the system prompt (instructions + body in a single field). A profile binds a Skill to a quality gate via its snapshot `gate_skills` map; the readiness/delivery judges inject the bound Skill's prompt as a rubric. Starter Skills may be seeded during deployment. Default seeding preserves existing user-managed records; overwrite seeding is an explicit operator refresh.

#### `GET /skills` — response JSON (canonical)

HTTP 200; Huma/OpenAPI envelope uses the key `**body**` (lowercase). **Do not** use the key `Body` (uppercase) in the current implementation — the Go struct has tag `json:"body"` to avoid divergence with clients that only read `body`.

```json
{
  "body": {
    "items": [
      {
        "id": "8f2c1b3a-…",
        "name": "prd-writing",
        "description": "Drafts and refines product requirement documents…",
        "prompt": "# PRD writing\n\n## Scope\n…",
        "created_at": "2026-04-18T12:00:00Z",
        "updated_at": "2026-04-18T12:00:00Z"
      }
    ]
  }
}
```

- `**items**`: array of all skills in the DB (not paginated). Order is not an API guarantee; clients filter by `name`.
- **Backward compatibility:** clients (e.g. the Python governance) may still encounter older responses with the key `**Body`**; they should accept both `body` and `Body` when parsing.

**Errors:** `503` if the skills service is not configured (`Skills == nil` in the process).

#### `POST /skills` — request body

Huma binds to `CreateSkillInput`: JSON payload has envelope `**body`** containing the following fields (full OpenAPI at `/openapi.json` when the server is running).

```json
{
  "body": {
    "name": "prd-writing",
    "description": "Short description for admin UI and MCP list.",
    "prompt": "# Title\n\nMarkdown instructions injected into governance system prompts."
  }
}
```

- `**name**`, `**prompt**`: required (on create).
- `**description**`: optional per handler validation.

Response 200: `{ "body": <SkillDTO> }` — same shape as an item in `items` above (includes `id`, timestamps).

#### `PUT /skills/{id}` — request body

Same envelope `**body**` as `POST` (replaces the entire record): `**name**`, `**prompt**` required; `**description**` per validation.

Response 200: `{ "body": <SkillDTO> }`.

#### `DELETE /skills/{id}` — response

```json
{
  "body": {
    "ok": true
  }
}
```

(Exact shape per OpenAPI generated from the handler; `ok` confirms successful deletion.)

---

### 6.8 CLI REST Facades (`/api/v1/`)

Versioned REST endpoints that expose the same operations as the embedded MCP tools, allowing CLI clients and non-MCP agents to drive the same governance workflow over plain HTTP. All routes are **open (internal network boundary only)** — no Bearer auth. They delegate to the shared `governanceops.Service` layer; no business logic lives in the handlers.

#### Build metadata

| Method | Path | Description |
|---|---|---|
| `GET /api/v1/meta` | Build info: `version`, `commit`, `date`, plus `recommended_cli_version` (the server's preferred `specgate` CLI version) and optional `minimum_cli_version`. Injected at link time via `-ldflags`; defaults to `dev`/`unknown`. | Open (internal) |

#### Governance status

| Method | Path | Description |
|---|---|---|
| `GET /api/v1/status` | Board phase counts — same result as `get_governance_status` MCP tool. Optional `?workspace_id=<id>` narrows counts and attention items to locally attributed work items in the selected workspace; it is a selection filter, not authorization. | Open (internal) |
| `GET /api/v1/stats` | Governance-value stats projected from existing `gate_runs` (change_request subjects), `change_requests`, and `governance_feedback_events` rows (no new tables or collection). Optional `?workspace_id=<id>` (joins each row's change request) and `?days=<n>` rolling window (default 30, clamped 1–365). Returns the flat metric fields below plus `window_days` and `workspace_id` echoes. | Open (internal) |

**Stats metrics** (window = rows with `created_at >= now − days`; delivery reviews are gate runs with `gate = delivery_review`; a "catch" is a run in state `fail` or `needs_human_review`):

- `reviewed_items` — change requests with at least one delivery-review run in the window.
- `first_pass` — reviewed items whose first delivery-review run **ever** passed (the CLI derives first-pass yield from this; delivery runs are read across all time so a pre-window failure keeps counting).
- `gate_catches_pre_build` — distinct failing `(change request, gate)` pairs among non-delivery gate runs in the window. Gate runs are point-in-time snapshots, so repeated runs against the same defect count once.
- `review_catches_post_build` — delivery-review runs in the window that are catches (each is a distinct review of a distinct submission); `review_catches_fixed` counts those whose change request has a later passing delivery review.
- `rework` — in-window delivery-review runs beyond the item's first review ever (resubmits); `items_with_rework` counts the items concerned.
- `ambiguity_blocks` — `coding_agent.blocked_ambiguity` feedback events in the window.
- `cycle_time_avg_hours` / `cycle_time_items` — for reviewed items whose latest delivery review passed, the average hours from change-request creation to the **first** passing review.
- `ledger` — newest-first list (max 20) of concrete catches: `{occurred_at, change_request_key, kind: gate_catch|review_catch|ambiguity_block, gate?, detail?}`, `detail` truncated to ~140 characters. Gate catches keep the latest snapshot per `(change request, gate)` pair.
- Known scope limit: pre-build catches cover change-request gate runs only; artifact readiness and IDE-agent gate-task failures are not yet counted.

#### Identity and workspaces

| Method | Path | Description |
|---|---|---|
| `POST /api/v1/identity/bootstrap` | Body `{workspace_name, display_name, username, email?}`. Normalizes and validates username, creates or reuses the user and workspace, creates an owner membership, and returns `{user, workspace}`. Uses internal IDs so later login/SSO providers can link to the same user. | Open (internal) |
| `GET /api/v1/users` | List local users ordered by username. | Open (internal) |
| `GET /api/v1/users/{id}` | Fetch a user by UUID or username. 404 when not found. | Open (internal) |
| `GET /api/v1/workspaces` | List local workspaces ordered by name. | Open (internal) |
| `GET /api/v1/workspaces/{id}` | Fetch a workspace by UUID or slug. 404 when not found. | Open (internal) |

#### Work items

| Method | Path | Description |
|---|---|---|
| `POST /api/v1/work-items/resolve` | Body `{ref}`: resolve a work reference (CR ID, CR key, issue URL, bare tracker key) to a canonical `{change_request_id, feature_id, ...}` envelope. 404 when the ref cannot be resolved. | Open (internal) |
| `GET /api/v1/work-items/{id}/context-pack` | Assemble the context pack for a change request; optional `?lane=fe\|be` filter. Returns `{state, summary, tasks_fe?, tasks_be?}`. 404 when the CR is not found. | Open (internal) |
| `POST /api/v1/work-items/{id}/feedback` | Record a coding-agent feedback event. Body is a `ReportFeedbackInput` payload; `change_request_id` from the URL path is authoritative (any body `change_request_id` is overwritten). Agent-supplied `source` fields on evidence items are stripped server-side. | Open (internal) |
| `POST /api/v1/work-items/{id}/readiness` | Run the readiness gates for a change request via the agents service. Returns `{pass, checks, hint}`. 503 when `AGENTS_BASE_URL` is unset. | Open (internal) |
| `POST /api/v1/work-items/{id}/llm-gates` | Trigger LLM quality gates for a change request via the agents service. Returns opaque `map[string]any`. 503 when `AGENTS_BASE_URL` is unset. | Open (internal) |
| `POST /api/v1/work-items/{id}/delivery-review` | Trigger the delivery review for a change request via the agents service. Returns opaque `map[string]any`. 503 when `AGENTS_BASE_URL` is unset. | Open (internal) |
| `POST /api/v1/work-items` | Create a quick-route change request from issue content via the agents service. Body: `{title, description, issue_url?, issue_key?, feature_key?, feature_name?, created_by?, workspace_id?, acceptance_criteria?: string[]}`. `feature_key` is optional; when omitted, the CR and quick Context Pack artifact stay featureless instead of inventing a durable Feature. `created_by` and `workspace_id` are cooperative attribution from the CLI's current local identity/workspace, not authorization. When `acceptance_criteria` is present, the quick Context Pack uses those exact trimmed criteria; otherwise the agents service drafts or falls back to a generic criterion. Returns opaque `map[string]any`. 503 when `AGENTS_BASE_URL` is unset. | Open (internal) |
| `POST /api/v1/work-items/{id}/archive` | Archive a work item by CR id, CR key, issue URL, or bare tracker key. Resolves the ref to the canonical ChangeRequest, sets `archived=true`, stamps `archived_by` from optional body `{actor}` (default `specgate-cli`), and persists optional `{reason}` into `archive_reason`. | Open (internal) |
| `GET /api/v1/work-items/{id}/gates` | Return the persisted gate state for a change request without re-running gates. Response `{change_request_id, gates: [{gate, state, hint?}]}`. 404 when CR not found. | Open (internal) |
| `GET /api/v1/work-items/{id}/gate-history` | Return gate run history. Optional `?gate=<name>` filter; `?limit=<n>` (1–200, default 20). Response `{change_request_id, runs: [{gate_run_id, gate, state, hint?, created_at}]}`. 404 when CR not found. | Open (internal) |
| `GET /api/v1/work-items/{id}/delivery-status` | Return the latest delivery review verdict. Optional `?detail=true` for per-criterion breakdown. Response `{change_request_id, found, verdict?, hint?, reviewed_at?, outstanding_md?, per_criterion?}`. 404 when CR not found. | Open (internal) |
| `GET /api/v1/work-items/{id}/policy` | Return the governance explanation for a work item. Resolves the work ref and, when `lead_artifact_id` is present, delegates to the artifact policy logic. Quick-route CRs without a lead artifact return a streamlined Context Pack + delivery-review policy explanation instead of 404. 409 on incompatible snapshot schema. | Open (internal) |

#### Artifacts

| Method | Path | Description |
|---|---|---|
| `POST /api/v1/artifacts/publish` | Publish a CLI/IDE artifact package through the governance service. Body is the service-level `governanceops.PublishArtifactInput` shape (`feature_key`, optional `base_version`, `documents`, source/governance metadata); the server owns the concrete artifact version and review hints. Returns 409 Conflict when `base_version` is stale (`ErrVersionConflict`). | Open (internal) |
| `GET /api/v1/artifacts/{id}/policy` | Return the governance explanation for a specific artifact from its persisted `gates_profile_snapshot_json`. Response is a `governanceprofile.Explanation` (`{governance_level, title, summary, reasons?, obligations?, policy_lineage?}`). 404 when artifact not found; 409 when the stored snapshot schema version is unrecognised (fail-closed). Empty snapshot returns a bare standard-governance explanation. | Open (internal) |

#### Policies

Policy endpoints expose the built-in governance policy resolver. They are pure read surfaces — no persistence, no artifact mutation.

| Method | Path | Description |
|---|---|---|
| `GET /api/v1/policies/levels` | List all three built-in governance tiers (`light`, `standard`, `enhanced`) with their execution projections (`governance_level`, `display_name`, `approval_policy`, `evidence_policy`, `required_roles`, `required_topics`, `required_evidence`, `enabled_gates`). Response: `{levels: [...]}`. | Open (internal) |
| `POST /api/v1/policies/resolve` | Dry-run: resolve the governance level and explanation for a proposed change. Accepts the same classification fields as artifact publish (`request_type`, `impact_level`, `requested_governance_level`, `impact_declaration`) but does **not** persist. Returns a `governanceprofile.Explanation`. | Open (internal) |

#### Gate Tasks

Gate-task routes expose the IDE-agent pull/submit lifecycle. They are registered conditionally: if `GateTaskStore` is nil (not configured), the routes are **not registered** and requests return 404.

| Method | Path | Description |
|---|---|---|
| `GET /api/v1/gate-tasks` | List gate tasks; optional `?artifact_id=` filter. Returns `{tasks: [{task_id, artifact_id, gate_key, gate_version, gate_digest, artifact_digest, profile_digest, executor, skill_content, expires_at}]}`. 404 when store not configured (routes not registered). | Open (internal) |
| `GET /api/v1/gate-tasks/{task_id}` | Get a single gate task with Skill content. Returns the full task record with fields: `task_id, artifact_id, gate_key, gate_version, gate_digest, artifact_digest, profile_digest, executor, skill_content, expires_at`. 404 when not found or store not configured. | Open (internal) |
| `POST /api/v1/gate-tasks/{task_id}/result` | Submit a GateResult. Body is the result JSON (executor-defined schema). Returns `{result_id, trust, state}`. 404 when task not found or store not configured. 422 for stale gate digest. 400 for executor mismatch. | Open (internal) |

#### Skills

| Method | Path | Description |
|---|---|---|
| `GET /api/v1/skills` | List skills; optional `?name=` case-insensitive prefix filter. Returns `{items: [...]}`. 503 when skills service is not configured. | Open (internal) |
| `GET /api/v1/skills/{id}` | Fetch a single skill by ID. 404 when not found, 503 when skills service is not configured. | Open (internal) |

**Error shapes:** all errors follow the Huma v2 RFC 9457 problem detail shape. 503 is returned when `AGENTS_BASE_URL` is unset and an agents-backed operation is requested, or when the skills service is nil. 404 maps to `governanceops.ErrNotFound`; 409 to `ErrVersionConflict`; 400 to `ErrProviderRequired`.

**Response format:** Huma v2 returns response fields at the JSON top level, not wrapped in a `body` key, plus a `$schema` field. Clients should parse the struct fields directly.

---

## 7. Access Control

Doc Registry is an internal service — no authentication at the HTTP layer. The trust model is network-boundary-based: the registry is only accessible from agents and UIs within the same deployment network (VPC / service mesh / internal cluster). Client identity is passed via request body (`created_by`, `approved_by`) for audit purposes, not to gate access.

### 7.1 Conceptual roles

The roles below are intended to describe who does what, not to enforce permission. The UI/orchestrator is responsible for invoking endpoints in line with the actor's role:


| Role                             | Actions                                              |
| -------------------------------- | ---------------------------------------------------- |
| `governance-ops`                  | publish artifacts (POST /artifacts), check conflicts |
| `fe-agent / be-agent / qa-agent` | read artifacts + fetch files                         |
| `approver` (human)               | PATCH status (approve / request changes)             |
| `admin`                          | delete, override retention                           |


### 7.2 Hardening (post-MVP)

When the registry needs to move outside the trust boundary, options include:

- mTLS for service-to-service
- Signed requests from the orchestrator
- Short-lived JWT + RBAC at the route level

Not implemented in MVP; the architecture must support adding these later without changing the API shape.

---

## 8. Event Schemas

Events are written to the `artifact_events` table on each status transition. Downstream consumers poll `GET /events` or receive webhook calls (post-MVP).

### `artifact.published`

```json
{
  "event_type": "artifact.published",
  "feature_id": "checkout-loyalty-points",
  "version": "v0.3",
  "status": "approved",
  "manifest_url": "https://registry/.../manifest.json",
  "impact_level": "high",
  "impacted_services": ["order-service", "loyalty-service"],
  "confidence_score": 0.88,
  "timestamp": "2026-04-16T10:00:00Z"
}
```

### `artifact.needs_changes`

```json
{
  "event_type": "artifact.needs_changes",
  "feature_id": "checkout-loyalty-points",
  "version": "v0.3",
  "review_rating": 2,
  "note": "Add rollback plan and clarify loyalty-service API contract",
  "requested_by": "tech-lead@company.com",
  "timestamp": "2026-04-16T10:30:00Z"
}
```

The UI/orchestrator uses this event to trigger or propose triggering the governance-ops service to regenerate a new version. Doc Registry only records the transition and emits the event; it does not run governance operations itself.

### `artifact.superseded`

```json
{
  "event_type": "artifact.superseded",
  "feature_id": "checkout-loyalty-points",
  "version": "v0.3",
  "superseded_by": "v0.4",
  "manifest_url": "https://registry/.../manifest.json",
  "timestamp": "2026-04-16T11:00:00Z"
}
```

Downstream agents must invalidate their local cache on receiving `artifact.superseded`.

Work-board state changes also append rows to `artifact_events` (`feature.canonical_changed`, `change_request.acceptance_criteria_changed` — see §15).

---

## 9. Retention Policy


| Status                             | Retention                                                    |
| ---------------------------------- | ------------------------------------------------------------ |
| `approved`                         | At least 180 days                                            |
| `superseded`                       | At least 90 days                                             |
| `needs_changes`                    | At least 30 days                                             |
| `draft` (active)                   | Retained indefinitely until reviewed or superseded           |


**Cleanup rules:**

- Retention cleanup is currently manual: there is no scheduled job that deletes artifacts past their retention window. Operators run cleanup ad-hoc until automation lands.
- S3 files are deleted in the same transaction as database metadata removal
- Admins may override retention via PATCH with an explicit delete flag

**Governance Files Retention:**

`governance_files` rows whose `last_used_at` exceeds the `governancefiles.ttl_days` setting (default 90) are purged together with their S3 objects by an in-process daily ticker; pending rows older than 1 hour are purged as orphaned presigns. The purger reads `governancefiles.ttl_days` from the settings service on every sweep, so a change via `PUT /settings` (or the Settings UI) takes effect without a restart.
Ready `governance_files` objects referenced by any `artifact_files.s3_path` are pinned by the published artifact and must not be purged by governance-file TTL cleanup; immutable artifact documents stay readable for the artifact lifetime, regardless of governance-file TTL expiry.
**Artifact bundle markdown** under `artifacts/{feature_id}/{version}/` or `artifacts/standalone/{artifact_id}/{version}/` is never touched by the governance-file TTL purger. Do not delete artifact markdown objects that the product team may still read — including older or superseded versions kept for audit.

---

## 10. Conflict Detection

Conflict detection is advisory: the registry queries active artifacts (`status: draft` or `approved`) sharing impacted services with a candidate via `GET /conflicts`, which callers (governance-ops service, UI) may use to pre-check before publishing. A detected conflict never changes the stored status — publish always proceeds with the requested status. `impact_level` does not replace conflict detection but may be used to prioritize review and queueing.


| State               | Meaning                                                                                               |
| ------------------- | ----------------------------------------------------------------------------------------------------- |
| `no_conflict`       | No overlap with any active artifact                                                                   |
| `warning_conflict`  | Overlap on the same service but different module, or different branch                                  |
| `blocking_conflict` | Overlap on the same service with an `approved` or pending-review `draft` artifact                       |


MVP: conflict detection at service level. Module-level is post-MVP and requires module mapping in `artifact_services`.

---

## 11. Scheduled Jobs

Doc Registry currently runs no scheduled jobs for artifacts. Auto-supersede, retention cleanup, and event fan-out are planned but not yet implemented; all status changes are performed by REST API callers (governance-ops service / admin).

---

## 12. Error Handling


| HTTP Status | Condition                                              | Response                                             |
| ----------- | ------------------------------------------------------ | ---------------------------------------------------- |
| `400`       | Invalid manifest schema or version format              | `{ error: string, field: string }`                   |
| `404`       | Artifact or file not found (`ErrNotFound` / `ErrFileNotFound`) | `{ error: "not_found" }`                             |
| `409`       | Version conflict — version already exists for `feature_id` (`ErrConflict`), or a supplied `base_version` is stale / no longer the latest (`ErrStaleBase`) | `{ error: "version_conflict", existing_id: string }` |
| `422`       | Invalid or unsafe document path                        | `{ error: string }`                                  |
| `500`       | Internal server or S3 error                            | `{ error: string, request_id: string }`              |


All 5xx errors are emitted to Sentry with `request_id`, `artifact_id` (if available), and failed operation context.

**Sentry configuration (HTTP server):** environment variables `SENTRY_DSN` (required to enable the SDK), `SENTRY_ENVIRONMENT` (default `development`), `SENTRY_RELEASE` (optional, e.g. git SHA), `SENTRY_TRACES_SAMPLE_RATE` (0–1, default `0` = performance tracing disabled). The `sentry-go/http` middleware reports panics; the `request_id` tag is set from chi's RequestID. When handlers return 5xx errors without panicking, they should call `sentry.CaptureException` (or equivalent) with the error and `artifact_id` context where applicable.

---

## 13. Implementation Notes — Go

- Storage driver: `DATABASE_DRIVER=postgres`. See `internal/storage/db/` for driver selection.
- S3 operations via `aws-sdk-go-v2`. Uses presigned URLs (15-minute expiry) for file delivery.
- All status transitions are wrapped in a database transaction — writes `artifact_events` in the same transaction as the status update.
- Conflict detection query:

```sql
SELECT DISTINCT a.feature_id
FROM artifact_services s
JOIN artifacts a ON s.artifact_id = a.id
WHERE s.name IN (?)
  AND a.status IN ('draft', 'approved')
  AND a.feature_id != ?
```

- Retention cleanup query — the Go job builds the cutoff in Go (`time.Now().UTC().Add(-30*24*time.Hour)`) and binds it as a parameter, so the SQL is dialect-neutral:

```sql
SELECT id FROM artifacts
WHERE status = 'needs_changes'
  AND updated_at < ?
```

  Cascade delete S3 files first, then remove database rows.

---

## 14. Governance Knowledge

Governance Knowledge lets users feed context into the Governance when creating or updating a spec. This is not a general document management system; the data only serves planning retrieval.

### 15.1 Supported inputs

- File upload: `.md`, `.txt` only (API rejects other extensions)
- `POST /documents/text` remains available for automation; the UI uses upload only

MVP parses/indexes `.md`, `.txt` (upload). GET document detail returns full `extracted.txt` in `extracted_preview` without a character cap.

### 15.2 Taxonomy and authority

`document_type`:

- `product_brief`
- `srs`
- `design_reference`
- `supporting_doc`
- `existing_artifact`
- `qa_finding`
- `policy_doc`

`authority_level`:

- `source_of_truth`
- `high`
- `reference`
- `low`

MVP keeps the HTTP no-auth model from §7. The UI/orchestrator is responsible for invoking endpoints in line with the actor's role. The backend only enforces lightly: when the request sends `actor_role`, `source_of_truth` requires `reviewer` or `admin`.

### 15.3 Versioning

- `document_id` represents the lineage.
- `(document_id, version)` represents one immutable content version.
- Content updates create a new version and mark the previous version `is_latest=false`.
- Reindex does not create a new content version.
- Default retrieval uses only `is_latest=true`; `include_history=true` is for audit/debug.
- MVP auto-versioning: a new document is `v1`, subsequent updates are `v1.1`, `v1.2`, ...

### 15.4 Storage

S3 layout:

```text
documents/{document_id}/{version}/
  raw/
    original.file
    input.txt
  processed/
    extracted.txt
    chunks.json
```

Database tables:

- `documents`: metadata per version, latest flag, source, links, status, summary, notes, tags, error message
- `document_chunks`: chunk records tied to `(document_id, version)`
- `document_links`: feature/request links tied to `(document_id, version)`

pgvector:

- PostgreSQL extension: `pgvector` (`CREATE EXTENSION IF NOT EXISTS vector`)
- Table: `knowledge_chunks` stores chunk embeddings (`embedding vector(1024)`) with a `payload JSONB` column for all metadata.
- Distance: cosine similarity via `<=>` operator
- Payload includes `document_id`, `version`, `title`, `is_latest`, `document_type`, `authority_level`, feature/request links, tags, source, and chunk text — all stored as JSONB in the same row as the embedding.

### 15.5 Processing states


| Status     | Meaning                        |
| ---------- | ------------------------------ |
| `uploaded` | Raw source saved               |
| `parsing`  | Extracting or normalizing text |
| `chunked`  | Text split into chunks         |
| `embedded` | Embeddings generated           |
| `indexed`  | Chunks upserted into pgvector  |
| `failed`   | At least one step failed       |


### 15.6 REST API


| Method + Path                                     | Description                                                             |
| ------------------------------------------------- | ----------------------------------------------------------------------- |
| `POST /documents/upload`                          | Upload file as new lineage or new version                               |
| `POST /documents/text`                            | Create text document as new lineage or new version                      |
| `GET /documents`                                  | List documents; latest-only by default unless `include_history=true`    |
| `GET /documents/{document_id}?version=`           | Get document detail, full extracted text (`extracted_preview`), history |
| `POST /documents/{document_id}/versions`          | Create a new text version                                               |
| `DELETE /documents/{document_id}?version=`        | Delete one version                                                      |
| `POST /governance/context/search`                    | Retrieve Governance Knowledge chunks                                       |


**MCP:** the `search_knowledge` tool on the embedded MCP calls the same backend retrieval as `POST /governance/context/search` (clients do not read raw S3). MCP input uses `limit` (default 5, max 20); output is a JSON string with `{ "items": [...], "truncated": boolean }`. Each item mirrors the REST result fields (`document_id`, `version`, `title`, `document_type`, `authority_level`, `chunk_text`, `score`, `source_uri`, `chunk_index`).

Search request:

```json
{
  "query": "loyalty points checkout rules",
  "linked_feature_id": "checkout-loyalty",
  "document_types": ["srs", "design_reference"],
  "max_chunks": 8
}
```

Search response:

```json
{
  "results": [
    {
      "document_id": "doc_123",
      "version": "v1.1",
      "title": "Checkout Loyalty SRS",
      "document_type": "srs",
      "authority_level": "source_of_truth",
      "chunk_text": "...",
      "score": 0.91,
      "source_uri": "documents/doc_123/v1.1/processed/extracted.txt"
    }
  ]
}
```

### 15.7 Implementation notes

- Chunk target: 400-800 token-ish words, paragraph/heading boundaries preferred.
- The embedding provider + model are configured in the app (**Settings → Model → Embedding Model**), not via env. Supported providers: `google_genai` (Gemini `embedContent`), `openai`, and `openrouter` (the latter two share the OpenAI-compatible `/embeddings` API); the API key reuses the provider's existing `*.api_key` setting. The embedder is resolved from settings at call time, so changes take effect without a restart, and the server **boots with embeddings disabled** when none is configured (knowledge read paths work; search/upload report `embeddings_enabled: false` so the UI warns and disables upload). `KNOWLEDGE_EMBEDDING_DIM` (default `1024`) is the pgvector column width; the chosen model must produce that width (providers that support a `dimensions`/`outputDimensionality` parameter are requested at this width). Switching to a model with a different native width requires re-indexing existing documents.
- pgvector search is post-filtered through Postgres for latest-version correctness.
- Governance retrieval priority should rank linked `source_of_truth`, linked `high`, related existing artifacts, `reference`, then `low`.

### 15.8 Blob storage

`STORAGE_DRIVER` selects the backend for binary governance-file uploads used by explicit attachment pins and compatibility flows:

| Driver | Backend | Notes |
| ------ | ------- | ----- |
| `local` | Local filesystem (`BLOB_DATA_ROOT`, default `/data/blobs`) | Default. No MinIO required. |
| `s3` | S3 / MinIO (`S3_ENDPOINT` + credentials) | Production default. |

`PUT /governance/files/upload` routes through `BlobStore` (local) or `s3.Client` (s3). The object key stored in `governance_files.object_key` is a UUID for local blobs or an S3 path for the s3 driver. `GET /governance/files/{id}/content` detects the backend by key shape (`looksLikeBlobID`: len==36, 4 hyphens).

Artifact documents use an artifact object store selected by the same driver: `local` writes key-addressed files under `BLOB_DATA_ROOT/artifacts`, while `s3` writes to S3 / MinIO using the configured key prefix. This keeps `artifact_files.s3_path` as the stable object lookup key in both modes.

Knowledge raw document bytes (`ObjectStore`) use `NullObjectStore` when `STORAGE_DRIVER=local` — pgvector chunk payloads are the source of truth for knowledge search in dev; raw source bytes are only stored when `STORAGE_DRIVER=s3`.

## 15. Artifact-Native Work Board

Doc Registry owns durable workboard records. The model is:

- `Feature`: optional durable product capability with `canonical_artifact_id`. Larger product work follows Feature → ChangeRequest → Artifact.
- `ChangeRequest`: planning work that may link to one Feature, or may be featureless for quick-route bugfix/small-CR work. `workspace_id` is optional workspace attribution for locally created work items; it supports selection/scoping surfaces but not authorization.

Board phase is derived by clients from ChangeRequest artifact pointers. No separate ChangeRequest status is stored.

Governance-board tables:

- `features`
- `change_requests`
- `acceptance_criteria`

Governance-board endpoints:

| Method + Path | Description |
| --- | --- |
| `GET/POST /workboard/features` | List or create Features |
| `POST /workboard/features/upsert-by-key` | Idempotent create-or-get Feature by stable key. Body: `{key (required), name?}`. Returns the Feature DTO. Used only when callers deliberately choose a feature-backed route; quick-route CR creation may omit `feature_key` and remain featureless. |
| `GET/PATCH /workboard/features/{id}` | Read or update one Feature. PATCH is sparse: `name`, `summary`, and `status` are persisted only when present and non-empty in the body. UI uses this to flip `status` between `candidate`, `planned`, `active`, `rejected`, `deprecated` for product-team status changes (mark as Live, Retiring, etc.). Status changes emit `feature.status_changed` into `workboard_lifecycle_events`. |
| `PUT /workboard/features/{id}/summary` | Persist the LLM-generated feature Overview narrative. Body `{summary_md, source_version?}` where `source_version` is the canonical artifact version the narrative was generated from. Returns the updated Feature DTO. Missing feature → 404. |
| `GET/POST /workboard/change-requests` | List or create ChangeRequests. `GET` hides archived items by default; pass `?include_archived=true` to include archived rows. Optional `?workspace_id=<id>` narrows the list to locally attributed work items in one workspace; it is a selection filter, not authorization. |
| `GET/PATCH /workboard/change-requests/{id}` | Read or update one ChangeRequest. PATCH treats the body as a sparse partial: `title`, `intent_md`, `work_type`, `governance_thread_id`, `acceptance_criteria_json`, and archive metadata (`archived`, `archived_by`, `archive_reason`) are only persisted when present and non-empty in the body. When `archived=true`, backend sets `archived_at` automatically and emits `change_request.archived` into `workboard_lifecycle_events`. A sparse PATCH cannot *clear* `archived` (a false bool is indistinguishable from omitted) — use the unarchive endpoint. |
| `POST /workboard/change-requests/{id}/unarchive` | Restore an archived ChangeRequest: clears `archived`/`archived_at`/`archived_by`/`archive_reason` and emits `change_request.unarchived` into `workboard_lifecycle_events`. Body `{actor?}`. |
| `GET /workboard/change-requests/{id}/acceptance-criteria` | List normalized AC rows (`id`, `change_request_id`, `text`, `done`, `source`, timestamps). The `acceptance_criteria_json` field on the ChangeRequest is also accepted and mirrored into rows. |
| `GET /workboard/change-requests/{id}/next-actions` | Return derived gate actions for a work item: `{ gate, state, hint }`. Gates currently include `spec_drafted`, `spec_approved`, `no_conflicts`, `knowledge_fresh`, `canonical_spec`, and `delivery_pack`. `delivery_pack` state: `pass` when `context_pack_artifact_id` is set; `not_applicable` when `lead_artifact_id` is set but no stored pack (full-route CRs assemble the pack on-read via the MCP resource); `pending` when neither is set (quick-route CRs must persist a pack artifact). |
| `POST /workboard/change-requests/{id}/gate-runs/refresh` | Persist a gate snapshot for the work item and return rows. Request body is optional (`{}` or empty), with optional `evaluations[]` (`gate`, `state`, `hint`, `confidence`, `judge_model`, `eval_suite_version`) from agent judges merged over deterministic defaults. `confidence` must be within `[0,1]`. An evaluation whose `gate` has no deterministic next-action (an **eval-only gate** — the LLM quality gates, and the post-build `delivery_review` verdict) is persisted straight from the evaluation; the per-criterion verdicts + automated checks ride its `evidence` string as JSON. Returns (`gate`, `state`, `hint`, optional `proposal_ref`, `evidence_json` with contract version, evaluator metadata, verdict, confidence, linked knowledge rows, and warning context). **Auto-transition side effect:** when a `delivery_review` evaluation with `state=pass` is persisted, the handler asynchronously (best-effort, synchronous with swallowed errors) closes any open tracker issues linked to the CR (`tracker_links` rows with `state=opened`) across all supported providers: **Linear** issues are moved to the team's first `type=completed` workflow state via the GraphQL `issueUpdate` mutation; **GitHub** issues are closed via `PATCH /repos/{owner}/{repo}/issues/{number}` with `state=closed, state_reason=completed`; **GitLab** issues are closed via `PUT /projects/{id}/issues/{iid}` with `state_event=close`. Errors are logged at warn level and never fail the gate write. Already-closed links and unknown providers are skipped (idempotent). If `governance.auto_archive_on_delivery_pass` is `"true"`, the same future persisted pass also archives the ChangeRequest with `archived_by=specgate-auto-archive` and `archive_reason=delivery review passed`; archive update failures are returned to the caller after the gate row is persisted. (per spec §auto-transition CR-0D60C43C, CR-E37492D1) |
| `GET /workboard/change-requests/{id}/gate-runs?limit=` | List persisted gate snapshots for one work item (newest first). |
| `POST /artifacts/{id}/readiness-runs/refresh` | Persist an artifact-scoped readiness snapshot and return rows. Request body is optional (`{}` or empty), with optional `evaluations[]` (`gate`, `state`, `hint`, `confidence`, `judge_model`, `eval_suite_version`, `evidence`) from the artifact-readiness runner. Returns newest-ready rows (`gate`, `state`, `hint`, `evidence_json`, `created_at`) for that artifact. |
| `GET /artifacts/{id}/readiness-runs?limit=` | List persisted artifact-scoped readiness rows for one artifact (newest first). |
| `POST /workboard/change-requests/{id}/lead-artifact` | Patch the ChangeRequest lead artifact pointer |
| `POST /workboard/change-requests/{id}/context-pack-artifact` | Patch the Context Pack artifact pointer |
| `GET /workboard/change-requests/{id}/tracker-links` | List the tracker issue links a handoff created for the work item — one per lane, each `{lane, identifier, url, state (opened/closed/removed), tracker_state}`. Backs the work-item "linked issues" surface (open in tracker, see live state incl. a deleted issue). |
| `GET /workboard/stale-warnings?feature_id=&change_request_id=` | Central stale-knowledge warnings. If both filters are omitted, returns warnings for all features. |

Closed stale-warning codes: `canonical_artifact_missing`, `canonical_artifact_unapproved`, `canonical_artifact_superseded`, `canonical_promotion_available`, `feature_summary_outdated`, `lead_artifact_superseded`, `linked_knowledge_newer`, `context_pack_stale`, `feature_deprecated`, `delivery_in_progress`, `tracker_status_conflict`, `tracker_priority_urgent`, `delivery_stale`. Context-pack assembly warning codes: `knowledge_provenance_unavailable` (knowledge document lookup failed; `knowledge_provenance` is `[]`).

`feature_summary_outdated` is emitted for a feature when it has a persisted Overview (`summary_md` non-empty) whose `summary_source_version` differs from the current canonical artifact `version`. Severity is `warning`. It clears when the summary is regenerated against the current canonical version (the agent writes both fields via `PUT /workboard/features/{id}/summary`). The Feature DTO carries `summary_md` and `summary_source_version` (both omitted when empty); the agent generates `summary_md` and saves `summary_source_version` as the canonical artifact's version string. Whether the agent auto-regenerates the Overview on canonical change is gated by the `governance.auto_feature_summary` setting (default `true`).

`delivery_in_progress` is emitted for a feature when it has at least one `integration_delivery_links` row with `external_type = merge_request` and `state = opened`. Severity is `info`. The warning clears automatically when the MR/PR merges or closes (the link state is updated by the webhook ingestion path). The UI renders this as a distinct blue chip, not as a stale warning.

`tracker_status_conflict` is emitted for a change request when its derived `tracker_status` (the latest `delivery.tracker_status_changed` feedback event, correlated by the payload `correlation_id` matched against the CR id or key) contradicts the git/MCP merge evidence — a merged `integration_delivery_links` row (`external_type = merge_request`, `state = merged`) keyed by `change_request_id`. Two contradictions emit it (severity `warning`): tracker `completed` with no merged delivery ("{Provider} marked this done, but no merge was detected."), or a merged delivery while the tracker is neither `completed` nor `canceled` ("A merge was detected, but {Provider} hasn't marked this done."). `{Provider}` is resolved from the `provider` field of the feedback event payload (e.g. "GitHub", "GitLab", "Linear"); falls back to "Tracker" when absent. Tracker status augments but never overrides the artifact-derived phase, so the warning is emitted only on a clear contradiction; a CR with no tracker event raises no warning.

`tracker_priority_urgent` is emitted for a change request when the latest `delivery.tracker_status_changed` feedback event for the CR carries a `priority` of `1` (urgent) or `2` (high) **and** the CR has not yet been handed off (its `context_pack_artifact_id` is empty). The `priority` field is embedded in the `payload_json` of every `delivery.tracker_status_changed` event across all providers. Severity is `warn`; message is "Linked tracker issue is marked urgent/high priority but this work item has not been handed off yet." The warning fires only on priorities 1 and 2; priority 0 (no priority set) never triggers it. The warning clears automatically once the CR is handed off.

`delivery_stale` is emitted for a change request when its most recent `delivery_review` gate run has `state = fail` and the gate run's `created_at` is older than the configured SLA threshold (default 7 days, controlled by `DELIVERY_SLA_DAYS` env var). Severity is `warning`; message includes the number of days and the date of the last failed delivery review, e.g. "Delivery review has been failing for 9 day(s). Last failed on 2026-06-11." The warning is not emitted when no delivery review history exists for the CR, when the most recent delivery review passed, or when the failure is within the threshold. It surfaces automatically in the `get_governance_status` MCP tool's attention list so operators can identify stuck work without polling individual CRs.

The ChangeRequest DTO carries two derived fields computed on read and never persisted: `phase` and `tracker_status`. `phase` uses the richer work-item readiness model: `Intake` (no lead + no governance-chat thread), `Draft` (governance-chat thread exists but no lead artifact yet), `Review` (lead artifact exists but is not approved), `Ready` (approved lead artifact, no Context Pack), and `Handoff` (Context Pack exists). When related rows are unavailable, the fallback remains pointer-only (`Intake`/`Ready`/`Handoff`). `tracker_status` is the raw tracker `state.type`, empty when no tracker event. Both are populated on the single-CR read and the list path.

`linked_knowledge_newer` is emitted for a feature when the latest indexed Governance Knowledge linked by feature id or feature key has an `updated_at` later than the canonical artifact approval timestamp. If the canonical artifact has no approval timestamp, compare against the artifact `updated_at`. Non-indexed knowledge is ignored so stale warnings only reference context Governance can retrieve.

Governance-board state changes may append Doc Registry events with payload `feature_id` and related ids:

- `feature.canonical_changed`
- `change_request.acceptance_criteria_changed`
- `feature.stale_warning_added` (reserved for persisted stale-warning writes)

## 16. Native Integrations

Native integrations are provider records owned by Doc Registry. The inbound MCP endpoint remains a tool/runtime bridge for IDE coding agents; native integrations are the workflow backbone for durable source-control state, project/repo mappings, and webhook inbox processing.

Database tables:

- `integrations`: provider-level connection metadata (`provider`, `name`, `status`, `base_url`, `config_json`, health fields, timestamps). The primary outbound-auth material lives here: `api_token_encrypted` holds an AES-256-GCM (`v1:`-prefixed) **recoverable** copy of a provider API token (Linear GraphQL / GitLab REST / GitHub REST + hosted MCP), set via `PUT /integrations/{id}/api-token`; write-only on the API, surfaced only as the derived boolean `has_api_token`. OAuth-backed source-control integrations additionally store `auth_method`, `oauth_access_token_encrypted`, `oauth_refresh_token_encrypted`, `oauth_expires_at`, `oauth_token_type`, `oauth_scope`, `oauth_account_id`, `oauth_account_name`, `oauth_account_email`, and `oauth_host_key`; the API exposes only derived `has_oauth_token` plus account metadata. Linear keeps a `LINEAR_WEBHOOK_SECRET` env secret (injected via `WithWebhookSecrets`) for inbound verification. GitLab/GitHub still retain an optional **integration-level** `webhook_secret_encrypted` plus `GET/PUT /integrations/{id}/webhook-secret` (+ `POST …/rotate` for GitHub) as a manual fallback, but the preferred product path is resource-scoped auto-provisioned webhooks. Linear's `Linear-Signature` is a bare-hex HMAC-SHA256 of the body (no `sha256=` prefix) verified via `integrations.VerifyLinearSignature` against `LINEAR_WEBHOOK_SECRET`. A Linear integration stores its MCP server URL (default `https://mcp.linear.app/mcp`) and `enabled` flag in `config_json`.
- `integration_oauth_states`: short-lived OAuth callback state rows (`state`, `integration_id`, `provider`, `host_key`, `redirect_target`, `expires_at`, `consumed_at`) used to validate callback CSRF and preserve the app-relative return target across the provider redirect.
- `integration_resources`: provider resources attached to one integration, e.g. GitLab projects or GitHub repos, with `external_key`, `external_id`, `default_ref`, `config_json`, and a per-resource encrypted webhook secret for auto-provisioned GitLab/GitHub webhooks.
- `integration_webhook_events`: append-only webhook inbox rows with provider, event type, external event id, `payload_hash` (hex SHA-256 of the raw body — a body-derived identity handle distinct from the provider delivery id, computed once in the repository so every recorded signal carries it), `correlation_id` (the SpecGate work item the signal declares via its `fixes SPECGATE-{key|id}` footer; empty when only fuzzy matching applied), JSON payload, receive/process timestamps, status (the processing result), and error text. A signal is recorded once it is authenticated and addressed to a known resource (i.e. it reaches the shared ingest pipeline); unauthenticated calls and unsupported-event-type / unmatched-resource pings are rejected or 200-ignored without a row, to avoid unbounded inbox noise.
- `integration_delivery_links`: durable links from governance work items to provider delivery objects, e.g. GitLab MRs per FE/BE repo, with state, branches, URL, and merge SHA.
- `tracker_links`: durable links from governance work items to **tracker issues** (Linear/GitLab/GitHub) created by a handoff. Distinct from `integration_delivery_links` (which is MR-shaped, requires a resource): tracker issues have no resource, carry a `lane` (`fe`/`be`/`""`), and live `opened`→`closed`→`removed`. Keyed `(integration_id, external_key)` — `external_key` (the human identifier, e.g. `ENG-123`, always present + lane-distinct) is the upsert/correlation key; `external_id` (the provider's immutable id, populated where the create response carries it — Linear) is a secondary correlation column. The handoff writes one row per lane; inbound tracker webhooks resolve the work item by this link (matched on `external_id`/`external_key`) **before** falling back to the editable `fixes SPECGATE-{key}` footer, so correlation survives a description edit that drops the footer. `tracker_state` holds the last inbound provider workflow state: the webhook path emits `delivery.tracker_status_changed` **only on a real transition** (a title/description edit re-delivers the same state and is ignored with `tracker_status_unchanged`), and advances `tracker_state` + the lifecycle `state` (completed/canceled → `closed`). A Linear issue deletion (top-level webhook `action: "remove"`) is surfaced as a terminal `removed` tracker state: it emits a `delivery.tracker_status_changed` with `tracker_state="removed"` (so the derived work-item chip clears — `latestTrackerStatus` returns the newest event's state) and moves the link to `state="removed"`.
- `governance_feedback_events`: queue of integration-derived feedback for the planning agent. Feedback targets Feature/ChangeRequest IDs when known and carries provider payload context.

Supported statuses:

- Integration status: `connected`, `disabled`, `error`.
- Webhook inbox status: `pending`, `processed`, `failed`, `ignored`.

Initial REST contract:

| Method + Path | Body / response |
| --- | --- |
| `GET /integrations` | Response `{items: Integration[]}` |
| `POST /integrations` | Body/response `Integration`; `provider` and `name` required; `status` defaults to `connected`; `config_json` defaults to `{}` and must be valid JSON when supplied. |
| `GET /integrations/{id}` | Response `Integration`; 404 when unknown. |
| `PUT /integrations/{id}` | Body/response `Integration`; replaces mutable metadata for the existing id. |
| `GET /integrations/{id}/resources` | Response `{items: Resource[]}`; 404 when integration unknown. |
| `POST /integrations/{id}/resources` | Body/response `Resource`; `resource_type` and `external_key` required; `integration_id` is taken from the path. For GitLab/GitHub project resources, if outbound auth is already configured the server also creates the provider webhook and returns `config_json.webhook_url`, `config_json.provider_webhook_id`, `config_json.webhook_status`, plus `has_webhook_secret=true`. |
| `DELETE /integrations/{id}/resources/{resource_id}` | Returns 204. Managed GitLab/GitHub resources delete upstream webhook first, then remove the local row. If upstream delete fails, the endpoint returns 502 and the local resource remains. |
| `GET /integrations/{id}/webhook-events?resource_id=&status=&limit=` | Response `{items: WebhookEvent[]}` newest first; default limit `100`, maximum `200`. |
| `POST /integrations/{id}/webhook-events` | Body/response `WebhookEvent`; `event_type` required; `provider` defaults from the integration; `status` defaults to `pending`; `payload_json` defaults to `{}` and must be valid JSON when supplied. |
| `POST /integrations/{id}/resources/{resource_id}/gitlab/webhook` | Resource-scoped GitLab webhook receiver. Uses the resource's stored signing token, validates the Standard Webhooks signature + timestamp, ignores deliveries whose `project.id` / `path_with_namespace` do not match the resource, then commits through the same webhook ingest pipeline as the integration-level route. |
| `POST /integrations/{id}/resources/{resource_id}/github/webhook` | Resource-scoped GitHub webhook receiver. Uses the resource's stored HMAC secret, ignores deliveries whose `repository.id` / `full_name` do not match the resource, then commits through the shared ingest pipeline. |
| `POST /integrations/{id}/gitlab/webhook` | GitLab webhook receiver. Body is the raw GitLab JSON (capped at 1 MiB; oversized bodies return 413). Phases run in order: (1) authenticate — validate the Standard Webhooks signing token (`webhook-signature` HMAC over `{webhook-id}.{webhook-timestamp}.{body}` against the per-integration `whsec_` token, plus timestamp recency), refusing with 401 when no signing token is configured, the timestamp is stale, or the signature mismatches; (2) parse the payload, filter on event type, look up the matching `integration_resources` row; (3) commit — `RecordWebhookEvent` + `UpsertDeliveryLink` + `CreateGovernanceFeedbackEvent` + `UpdateWebhookEventStatus` all run inside one DB transaction. Replays dedup by `X-Gitlab-Event-UUID` (DB-level partial unique index) or by `sha256:` of the raw payload when the header is absent; both `processed` and `failed` prior outcomes count as duplicates (`ignored_reason=duplicate_webhook_event` or `duplicate_webhook_event_previously_failed`) so GitLab's redrive retries cannot double-fire governance feedback. |
| `POST /integrations/{id}/github/webhook` | GitHub peer. Body is the raw GitHub JSON (capped at 1 MiB; 413 on oversize). Phases: (1) authenticate — verify `X-Hub-Signature-256` HMAC over the raw body against the per-integration generated webhook secret, refusing with 401 when no secret is configured or it mismatches; (2) filter to `pull_request` events (others 200-ignored), resolve the repo resource by `repository.id` / `full_name`; (3) commit through the shared `commitDelivery` pipeline (same `RecordWebhookEvent` + `UpsertDeliveryLink` + `CreateGovernanceFeedbackEvent` + `UpdateWebhookEventStatus` transaction as GitLab). Replays dedup by `X-GitHub-Delivery` or `sha256:` of the raw payload. |
| `GET /governance/feedback-events?status=&change_request_id=&artifact_id=&limit=` | Response `{items: GovernanceFeedbackEvent[]}` newest first; default limit `100`, maximum `200`. |

**Async webhook processing.** When `REDIS_URL` is set, inbound webhook receivers
authenticate the delivery **synchronously** (bad signature/token → 401, never
queued), then **enqueue** a secret-free `webhook:deliver` task on an **asynq**
queue and return 200 immediately. An in-process worker (`internal/webhookqueue`,
concurrency `WEBHOOK_QUEUE_CONCURRENCY` default 10) drains the queue and runs the
same normalize + match + single-tx `commitDelivery` pipeline; failures retry up to
`WEBHOOK_QUEUE_MAX_RETRY` (default 5). The pipeline is idempotent (`RecordWebhookEvent`
dedups on the provider replay key), so retries can't double-fire feedback. When
`REDIS_URL` is **unset**, receivers fall back to **inline** processing (the
synchronous path). The task payload carries only the raw body + headers the
provider sent — never a decrypted secret.

**Notifications.** Notification state is durable and poll-first. Doc Registry
does not run a separate long-lived notification server in the default stack.
Consumers derive attention state from persisted rows:

- `GET /governance/feedback-events?status=pending&limit=...` for work-item
  feedback that needs human or delivery-review action.
- `GET /integrations/{id}/webhook-events?status=...&limit=...` for integration
  ingestion status and troubleshooting.
- Work Board and artifact list/detail endpoints for aggregate counts and current
  lifecycle state after a refresh.

Services may publish compact invalidation signals through
`internal/notifications.Publisher` after the durable transaction commits. The
current default publisher is nil; producers therefore never block on notification
delivery, and clients must treat polling as authoritative. The reserved signal
types are `feedback.recorded` with `{change_request_id, event_type}` and
`webhook.delivery.processed` with `{provider, integration_id, resource_id,
feature_id, change_request_id, status, feedback_event_ids}`. A future push
adapter may implement the same publisher interface, but it must remain a
best-effort cache invalidation path over the persisted API resources, not a
source of truth.

GitLab-first workflow intent:

- GitLab connection metadata belongs in `integrations` / `integration_resources`, not `settings`, so future providers can share the same UI and storage pattern.
- Merge request webhooks should be recorded in `integration_webhook_events` first, then provider-specific processors can link MRs back to governance ChangeRequests and update Context Pack / artifact pointers after merge.
- GitHub is a first-class peer of GitLab (per spec §FR-5.1). `POST /integrations/{id}/github/webhook` authenticates with HMAC-SHA256 (`X-Hub-Signature-256`) over the raw body using the per-integration generated webhook secret. It routes three event types: `pull_request` events run `commitDelivery` (same pipeline as GitLab); `issue_comment` events run the scope-drift comment path; `workflow_run` events run `commitCIRun` (see below). All others are ignored. PR `opened`/`reopened`/`synchronize` → `opened`; `closed`+`merged` → `merged`; `closed`+not merged → `closed`. Feedback uses the shared provider-neutral `delivery.pr_*` event names; delivery-link external keys are `repo#number` for GitHub vs `path!iid` for GitLab. Both providers share dedup, transactional commit, and unmatched/duplicate handling.
- **CI webhook path** (`workflow_run` events). When GitHub sends `workflow_run.completed` with `conclusion=success`, `commitCIRun` records a webhook event, matches the CR by branch name (same two-pass matching as PR delivery — `fixes SPECGATE-{key}` footer first, then fuzzy branch/URL scan), and creates a `delivery.ci_passed` feedback event **without** creating a DeliveryLink (no PR involved). Non-success conclusions (`failure`, `cancelled`, `skipped`, etc.) are silently ignored (`workflow_run_not_success`). Unmatched workflow runs are recorded as `ci_run_unlinked_to_work_item`. The delivery review reads `delivery.ci_passed` events to build the `evidence` array (kind=`ci_run`) in the gate run output so the UI's `deriveTrustTier()` can promote the evidence trust tier from `agent_reported` to `ci_verified`.
- MR-to-issue enforcement is a GitLab processor concern layered above the generic inbox: the generic table records what arrived; GitLab-specific logic decides whether the MR is linked to a known handoff issue and which governance records should be refreshed.
- Multi-repo delivery is represented by multiple `integration_resources` under one GitLab integration. A single ChangeRequest can therefore collect one FE MR link and one BE MR link without conflating repo state.
- MR matching runs in two passes (per spec §FR-5.3). First, an explicit `fixes SPECGATE-{change_request_key_or_id}` footer in the MR description or title links to that exact ChangeRequest and wins over any fuzzy coincidence (`fixes` and the `SPECGATE-` prefix are case-insensitive; the captured key/id is normalized before comparison). If no footer matches, it falls back to the conservative fuzzy scan: the MR title, description, source branch, or URL must contain the ChangeRequest id or key (for example `CR-LOYALTY-V1` or `cr-loyalty-v1`). Linked MRs create `delivery.pr_opened` / `delivery.pr_merged` feedback; unlinked MRs create `delivery.pr_unmatched` feedback and do not create delivery links.
- A delivery closed without merging additionally emits a `delivery.pr_closed` **review warning** (per spec §FR-5.5) — alongside the normal `delivery.pr_opened`, since the delivery link did change to `closed`. This is a signal for a human to review possible abandonment; the webhook never rolls back or mutates Feature/ChangeRequest state (§FR-5.6). The shared `commitDelivery` pipeline emits it for both GitLab and GitHub. `delivery.pr_merged` is the merge signal the reconciliation loop turns into a reviewed proposal whose **approval** advances the linked Feature `planned → active` (see §the verdict reconciliation note); the webhook itself never changes Feature status.
- GitLab retry idempotency uses `X-Gitlab-Event-UUID` when present and `sha256:` of the raw payload as a fallback dedup key when the header is missing (older GitLab releases or self-hosted runners). Both `processed` and `failed` prior outcomes count as duplicates so the retry path never re-fires governance feedback rows or attributes fresh side-effects to a stale webhook event id.
- Context Pack freshness remains artifact-first: webhook handling does not rewrite approved artifacts or promote canonicals. If a linked ChangeRequest already has a Context Pack artifact and delivery changes, the webhook emits `planning.context_pack_stale` feedback so the planning agent can regenerate or propose an artifact update.
