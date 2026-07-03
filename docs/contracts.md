# Contracts

This is the cross-module reference for shared SpecGate contracts. Use it when a
change affects more than one module, when a CLI/UI/governance-ops payload must
stay stable, or when release validation needs a canonical behavior statement.

For concepts, start with [How SpecGate works](concepts/how-specgate-works.md).
For command usage, use the [CLI reference](reference/cli.md).

## HTTP API dialects

Doc Registry serves two intentional HTTP surfaces, split by audience:

- **`/api/v1/*` — the CLI facade.** A curated, versioned contract for the
  `specgate` CLI, which is installed and updated independently of the server
  and so can see version skew (`/api/v1/meta` advertises the recommended and
  minimum CLI versions). Covers the CLI's nouns: work items, artifacts (list /
  get / files / publish / proposals / policy / gate-preview / gate-tasks),
  gate-tasks, skills, users, workspaces, status, stats, policies, identity.
- **The unversioned surface — the internal API.** The richer surface consumed
  by the web UI and the governance agent, which ship and deploy alongside the
  registry with no version skew: `workboard/*`, `artifact-edit/*`, `features`,
  `documents`, `integrations`, `governance/*`, and the artifact / gate-run
  detail endpoints.

Each client uses one dialect per noun. The two surfaces are not duplicates of
each other — where a noun appears on both (e.g. artifacts), the `/api/v1`
entry is the CLI's read subset, not a second copy of the internal API.

## Artifact identity

- `feature_id` is the stable business identifier
- `version` follows `vMAJOR.MINOR`
- An artifact is identified by `feature_id + version`

## WorkBoard identity

| Field | Meaning | Example |
| --- | --- | --- |
| `Feature.id` | Durable workboard UUID/key used by `/workboard/features/{id}` and ChangeRequest `feature_id` | `feature-uuid` |
| `Feature.key` | Human-stable product capability key; artifact `feature_id` uses this value when generated from workboard extraction | `FEAT-CHECKOUT` |
| `Feature.canonical_artifact_id` | Doc Registry artifact UUID for the canonical spec | `artifact-uuid` |
| `ChangeRequest.id` | Durable workboard work-item id used by `/workboard/change-requests/{id}` | `cr-uuid` |
| `Artifact.id` | Doc Registry artifact UUID | `artifact-uuid` |
| `Artifact.feature_id` | Stable business identifier for artifact versioning, usually `Feature.key` | `FEAT-CHECKOUT` |

## Artifact statuses

- `draft`
- `needs_changes`
- `approved`
- `superseded`

## Event types

- `artifact.published`
- `artifact.needs_changes`
- `artifact.superseded`
- `feature.canonical_changed`
- `change_request.acceptance_criteria_changed`

## Review contract

- Approve means the artifact can be consumed by downstream agents
- Request changes sends the artifact back for revision (status → `needs_changes`) and drafts a reviewed `review_request` artifact-update proposal from the reviewer's note; approving that proposal produces the new version
- The review note is optional — when blank, the governance-ops drafts a generic "address reviewer feedback" revision
- Manual artifact edits open a reviewed `manual_edit` proposal (not an in-place apply); approving it produces the new version. All artifact mutations flow through the one proposal → review → version loop

## Impact contract

- `impact_level` is an overall artifact-level signal, not a replacement for impacted services/apps
- Allowed values: `low`, `medium`, `high`
- The governance-ops should set `impact_level` based on blast radius, cross-service coupling, schema/API surface area, rollout complexity, and expected coordination cost
- `low` means a narrow, localized change with limited coordination
- `medium` means a change with moderate coordination or more than one touched surface
- `high` means broad blast radius, cross-service or cross-app coordination, or a risky rollout
- Impact level should be stored alongside the artifact manifest and surfaced in UI, queueing, and approval heuristics

## Artifact package contract

An artifact package is **flexible documents inside a fixed governance envelope**. The documents can be any structure an authoring tool produces (PRD/spec/BE/FE/QA; OpenSpec proposal/specs/design/tasks; Spec Kit spec/plan/research/data-model/tasks; a custom set). Doc Registry remains the source of truth for lifecycle, versioning, and access control regardless of the document layout.

**The fixed contract (envelope), not the filenames:**

- `work_item` / `feature` linkage, `version`, `status`, `approved_by` — the lifecycle identity.
- `source_kind` + `source_id` + `source_revision` — provenance/lineage (which tool/commit produced this).
- `authority` — what the package authoritatively represents (e.g. `product_intent`).
- `gates_profile` — the resolved readiness profile key that applies.
- `gates_profile_version` + `gates_profile_digest` + `gates_profile_snapshot_json` — the immutable resolved profile snapshot used by later readiness/handoff consumers.
- `canonical` — derived from `Feature.canonical_artifact_id`.

**Documents** are persisted as `[{ path, role }]` where `path` is the open document identity (also the S3 object name) and `role` is the governance vocabulary:

| role | the one job |
| --- | --- |
| `spec` | the governed source of truth — what/why, scope, non-goals, acceptance criteria (the hero role; a package with no `spec` document is incomplete) |
| `design` | how it's built — architecture, data model |
| `plan` | implementation breakdown / tasks |
| `verification` | test plan + evidence |
| `research` | background / exploration / decisions |
| `reference` | supporting, non-normative (refs, manifest, rollout/risks notes) |
| `unspecified` | role omitted on publish → auto-classified |
| `custom:*` | framework-specific role, passed through |

Many documents may share a role. Roles are **declared by the publisher** (the authoring IDE knows what it wrote); the platform classifies a document only when its role is `unspecified` — never by filename pattern. Risks/rollout/constraints are *topics* readiness gates locate wherever they live, not roles.

A flat **`files` map** (keyed by fixed keys such as `prd/spec/design/…`) is also accepted on publish and translated to `{path, role}` server-side. Framework conventions (OpenSpec, Spec Kit) are format choices, not runtime dependencies: approval, versioning, events, and downstream consumption remain governed by Doc Registry and these contracts.

## Governance Knowledge contract

Governance Knowledge documents are contextual material uploaded by humans so the governance-ops can generate or update artifacts with more business context. They are distinct from approved artifact package files.

- A knowledge document lineage is identified by `document_id`
- A knowledge document version is identified by `document_id + version`
- Only one version in a lineage may have `is_latest = true`
- Default retrieval returns latest versions only
- Older versions remain available for audit/debug when `include_history=true`
- Content updates create a new version; reindexing does not create a new content version
- The governance-ops must retrieve knowledge through `POST /governance/context/search`, or through the Doc Registry MCP tool `search_knowledge` when it is configured with `MCP_SERVER_URL` and `MCP_API_KEY` (same retrieval semantics; no raw S3 reads)
- If retrieved sources conflict, the governance-ops must surface the conflict instead of silently merging contradictory rules

Allowed `document_type` values:

- `product_brief`
- `srs`
- `design_reference`
- `supporting_doc`
- `existing_artifact`
- `qa_finding`
- `policy_doc`

Allowed `authority_level` values:

- `source_of_truth`
- `high`
- `reference`
- `low`

Knowledge processing statuses:

- `uploaded`
- `parsing`
- `chunked`
- `embedded`
- `indexed`
- `failed`

Governance context search request:

```json
{
  "query": "loyalty points checkout rules",
  "linked_feature_id": "checkout-loyalty",
  "document_types": ["srs", "design_reference"],
  "max_chunks": 8
}
```

Governance context search response:

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

Embedded MCP tool `search_knowledge` uses the same retrieval backend with input `limit` instead of REST `max_chunks` and returns a JSON string shaped as `{ "items": [...], "truncated": boolean }`. Each item carries the same result fields above; `authority` is also emitted as an alias for `authority_level`.

## Artifact publish contract (governance-ops → doc-registry)

The governance-ops uses two doc-registry REST endpoints for artifact lifecycle management:

- **`POST /artifacts`** — creates a new artifact record (S3 upload + database insert). Called twice per run:
  1. `draft_persist` node (Layer D.5): creates with `status=draft` immediately after draft generation.
  2. `publishing_handoff` (Layer F): fallback path when `draft_artifact_id` is absent (draft persist skipped or failed).
  Payloads may send small artifact files inline in `files` or reuse already-committed governance uploads via `file_refs` (`artifact_file_key -> governance_file_id`). `files` and `file_refs` must not both provide the same artifact file key.
- **`PATCH /artifacts/{id}/status`** — promotes an existing draft artifact. Called by `publishing_handoff` when `draft_artifact_id` is present in state. Accepted target statuses: `approved`, `needs_changes`, `superseded`. Does **not** accept `draft` (artifact is already draft when created by `draft_persist`). The governance-ops may include an updated `manifest` string so Doc Registry can keep the stored `manifest.json` in sync with the promoted status.
- **`POST /workboard/change-requests/{id}/lead-artifact`** then **`POST /workboard/change-requests/{id}/promote-lead-artifact`** — after a successful approved publish, the governance-ops patches the ChangeRequest lead artifact and promotes that approved lead artifact to `Feature.canonical_artifact_id`. This keeps Feature Map, quick handoff eligibility, and WorkBoard readiness aligned with the artifact source of truth. Non-approved statuses may still patch/link the lead artifact but must not become canonical automatically.

**Version format:** doc-registry enforces `^v\d+\.\d+$`. The governance-ops increments the minor component (`v0.1 → v0.2`) on each refine cycle. Suffixes like `-r1` are rejected with HTTP 422.

**Optional scores:** `confidence_score` and `ambiguity_score` are omitted from draft-persist payloads until the governance-ops has computed real numeric values. The governance-ops must not send JSON `null` for these optional fields because doc-registry validates the request body before artifact creation.

**Impact fallback:** Phase 1 draft persistence may run before confirmed impact analysis. In that case the governance-ops sends `impact_level="medium"` to satisfy Doc Registry's required enum instead of sending an empty string. The later impact pass remains authoritative for implementation planning and publish gating.

**Soft failure:** if doc-registry is unreachable during `draft_persist`, the governance-ops logs a warning and continues to the HITL step. `draft_artifact_id` is `None`; `publishing_handoff` falls back to `POST /artifacts`.

**Duplicate artifact recovery:** if `POST /artifacts` reports a duplicate `(feature_id, version)` either as a conflict status or in an HTTP error body, the governance-ops looks up the existing artifact with the same feature/version and reuses its id. During draft persistence this preserves the id for later publish promotion; during final publish fallback this promotes/reuses the existing artifact when the target status is patchable.

## IDE MCP publish contract (SpecGate)

IDEs publish spec packages via the `specgate_publish` MCP tool using a stable `feature_key`. SpecGate creates or references the Feature by key, auto-versions (`vMAJOR.MINOR`), records revision lineage (`parent_artifact_id` → the immediately preceding artifact id; `lineage_root_id` → the first artifact in the chain), and lands the artifact as `draft` for human review. Governance applies as always — a draft must be approved by a human before it becomes canonical or reaches downstream agents.

Profiles are resolved at publish time. `gates_profile` is not an opaque caller string: an explicit profile key must exist in the SpecGate registry, and when it is omitted SpecGate applies the deterministic fallback `bugfix -> bug_fix`, else `impact_level=high -> high_impact_feature`, else `generic_change`. Authoring provenance (`source_kind`) must never affect this resolution.

Artifacts published via `specgate_publish` carry two lineage fields on the `artifacts` row:

| Field | Description |
| --- | --- |
| `parent_artifact_id` | Id of the immediately preceding artifact in the revision chain; empty for the first artifact in a chain |
| `lineage_root_id` | Id of the chain's first artifact; self-referencing for chain roots |

Artifacts also snapshot the resolved governance profile so future readiness/handoff passes reproduce the exact historical contract even if the registry later changes:

| Field | Description |
| --- | --- |
| `gates_profile` | Resolved profile key (built-in bare key, or imported `namespace/key`) |
| `gates_profile_version` | Resolved immutable profile version |
| `gates_profile_digest` | Digest of the normalized resolved profile definition |
| `gates_profile_snapshot_json` | Normalized resolved profile payload used by later consumers |

SpecGate exposes two additional MCP tools for IDE plugins:

- `specgate_list_profiles` — list built-in and imported active profiles available for publish.
- `specgate_import_profiles` — import immutable team profile definitions into the registry so later publishes can reference them by stable key.

Verify the connection with `specgate_whoami` before publishing. Both tools are registered on the SpecGate MCP endpoint when the WorkBoard is enabled.

## Doc Registry MCP

The MCP endpoint is the **governance-ops service boundary**. Coding agents (Claude Code, Cursor, Codex) use the `specgate` CLI instead — see [guides/coding-agent-workflow.md](guides/coding-agent-workflow.md).

- MCP configuration lives in the **`settings`** table (see `GET /settings` / `PUT /settings`). Secrets are encrypted at rest with `SETTINGS_ENCRYPTION_KEY` (required env on the doc-registry process).
- Streamable MCP is mounted on the same process and port as the REST API at **`/mcp/stream`** (e.g. `http://localhost:8080/mcp/stream` when the registry listens on `:8080`).
- Governance-ops agents use **streamable HTTP**: set `MCP_SERVER_URL` to that full URL and Bearer token matching `mcp.api_key` in settings.
- Repo operations source GitLab repos from connected GitLab **integrations** (provider=`gitlab` × their project resources), not from settings keys or a local `REPO_ROOT` clone.
- `GET /mcp/info` returns live status, `restart_required`, and tool catalog (no raw secrets); the `repo_*` tools appear iff a GitLab integration repo is connected.
- `repo_context_pack` is the preferred first-pass repo MCP tool when available; it returns a compact cached context bundle for a query before the governance-ops drills into symbols or snippets.
- Embedded MCP resources include `specgate://context-pack/{change_request_id}` for normalized handoff payload reads (`change_request`, `feature`, `warnings`) without publishing or mutating artifacts.
- WorkBoard gate snapshots use `POST /workboard/change-requests/{id}/gate-runs/refresh` and `GET /workboard/change-requests/{id}/gate-runs` to persist/read gate rows. Refresh accepts empty body for deterministic-only snapshots and optional `evaluations[]` from agent judges (`gate`, `state`, `hint`, `confidence`, `judge_model`, `eval_suite_version`) that override deterministic defaults for that run; `confidence` outside `[0,1]` is rejected with HTTP 400. Rows include (`gate`, `state`, `hint`, optional `proposal_ref`) and `evidence_json` containing source artifact, evaluator metadata (`judge_model`, config/eval versions), verdict, confidence, linked knowledge versions, and warning context.
- Artifact-scoped readiness snapshots use `POST /artifacts/{id}/readiness-runs/refresh` and `GET /artifacts/{id}/readiness-runs`. The agents runner surface is `POST /artifacts/{id}/run-readiness`: it loads the artifact's published document bundle plus `gates_profile_snapshot_json`, threads profile `enabled_gates` and `required_topics` into the readiness harness, then persists per-gate rows (`gate`, `state`, `hint`, `evidence_json`, `created_at`) scoped to that artifact.
- WorkBoard gate proposals can be materialized with `POST /workboard/change-requests/{id}/gate-proposals/session` (`gate=knowledge_fresh|canonical_spec`), returning a concrete Artifact Edit session payload so clients do not parse URL strings.
- Artifact reads use explicit artifact ids from work items, Context Packs, or search results with `artifact_read_bundle(artifact_id, files?, max_chars?)`; governance-chat threads do not carry artifact links.

## Doc Registry settings API

- `GET /settings` returns all keys; sensitive values appear as `***` unless the request includes `Authorization: Bearer` with the same token as `mcp.api_key` in settings (full decrypted map for trusted MCP clients).
- `PUT /settings` accepts `{ "settings": { "key": "value", ... } }`. Sending `***` for a sensitive key leaves the stored value unchanged.
- `governance.auto_archive_on_delivery_pass` is a non-sensitive boolean string setting, default `"false"`. When enabled, persisting a future passing `delivery_review` gate run archives the ChangeRequest with backend-owned audit metadata (`archived_by=specgate-auto-archive`, `archive_reason=delivery review passed`). It does not delete work, artifacts, evidence, or chat threads.

## Governance-ops contract

The `governance` LangGraph graph exposes a single deep-agent node (`governance_chat.py`).
It binds four governance read/run tools (`get_artifact`, `get_artifact_documents`,
`list_artifact_readiness`, `run_artifact_readiness`) and handles domain questions about
gate failures, version comparisons, and spec conflicts. No drafting, no HITL interrupts.
See `app/agents/docs/governance/docs/spec.md` for the full contract.

- External trackers are downstream execution mirrors. Linear, GitHub Issues, GitLab Issues, Jira, or similar tools may hold assignment, comments, cycle/project state, and coding-agent delegation, but tracker state must not replace the approved artifact or Context Pack as the implementation contract.
- Tracker, git, QA, coding-agent, or user feedback must become evidence, conflict, or a reviewed proposal before it changes an approved artifact.
- Files, screenshots, images, or logs shared inside an IDE/coding-agent session
  are not automatically SpecGate attachments. They remain session-local
  implementation context unless explicitly pinned to a feature as governed
  supplemental evidence with an audience (`gate`, `coding_agent`, or `both`).
  Material that changes the source-of-truth contract must enter the reviewed
  artifact-proposal flow instead of becoming an attachment.

## Governance-ops stream contract

The governance-chat node streams over standard LangGraph channels:
- **Transcripts** ride the `messages` / `messages-tuple` channel (standard assistant replies).
- **Durable state** (such as thread title) rides `values` / `updates` frames.
- No custom companion events, no canvas payload, no HITL interrupt shapes.
See `app/agents/docs/governance/docs/event-contract.md` for the wire shapes.

## Artifact-native workboard contract

The Feature is the top product layer: Feature → ChangeRequest → Artifact. The product does not group work under Initiatives; extraction and approval operate on Features and ChangeRequests only.

- Doc Registry owns durable `Feature` and `ChangeRequest` and extraction approval records under `/workboard/*`.
- Agents expose `/agents/workboard/extractions` and `/agents/workboard/extractions/{id}/approve`; extraction uses structured model output for proposed Features and ChangeRequests, while UI approval calls the agent once and the agent delegates durable writes to Doc Registry. If model extraction is unavailable, agents persist an empty proposal envelope instead of heuristic text parsing.
- `/agents/workboard/extractions` accepts `source_kind="chat"` and an optional prepared `proposals` envelope. Prepared proposals are persisted directly and bypass extraction LLMs — e.g. a chat-prepared `existing/new/spike/unknown` Feature classification is carried straight through to approval.
- For existing Feature matches, the backend approval adapter sends Doc Registry `ExtractionApproval` with the path `extraction_id` repeated in the body, `FeatureApproval.decision="bind_existing"`, `existing_feature_id=<Feature.id>`, and nested `change_request`. UI resume payloads stay decision-oriented and do not need to know this Doc Registry shape.
- Governance-ops may curate the workboard only through **reviewed operations** — the extraction-approval flow (above) and the reviewed-proposal loop (`ProposalReviewPanel`) — never freeform writes to the durable tables. Allowed operations: create candidate/planned Feature, bind a new ChangeRequest to an existing Feature, and patch lightweight metadata on candidate/planned Features. Active Feature renames, merges, splits, deprecations, and silent taxonomy mutations are forbidden. The governance principle rides extraction approval + reviewed proposals.
- Quick handoff is a first-class lane for approved low-risk existing work (proportional ceremony). It creates an approved Context Pack artifact, patches `ChangeRequest.context_pack_artifact_id`, leaves `lead_artifact_id` empty, and does not auto-start implementation.
- The quick lane route is **classifier-suggested, human-confirmed**. `POST /workboard/change-requests/{id}/classify-route` runs an LLM classifier over the work item (title, intent, work type, the feature's best-effort impact level) and returns `{route: quick|full, confidence, rationale}` — a structured-output decision (no keyword/rule matching over user content), conservatively defaulting to `full` on low confidence or any error. It commits nothing. The human confirms or overrides on the work item: confirming **quick** calls `POST /workboard/change-requests/{id}/context-pack` with `{quick_mode: true}` (the approved quick-mode Context Pack, no full PRD/spec bundle); confirming **full** runs the normal PRD→Spec→FE/BE/QA flow. The context-pack endpoint defaults to `quick_mode: false` when no body is sent (back-compat). Trackers and the quick lane stay optional — neither is required to complete a work item.
- `Feature.canonical_artifact_id` is the product-memory pointer. Initial extraction assignment does not increment `Feature.version`; replacing the canonical artifact does.
- ChangeRequest board phase is derived on read and never persisted. The fallback is still pointer-based (`no lead artifact = Intake`, `lead artifact present = Ready`, `context pack present = Handoff`), but the hydrated read model upgrades that into the richer product review flow when more context is available: `governance_thread_id` with no lead = `Draft`, non-approved lead artifact = `Review`, approved lead artifact = `Ready`, context pack present = `Handoff`, and a latest passing `delivery_review` gate run = `Delivered` (server-side override; the not-yet-archived state between handoff and archive — the optional auto-archive-on-delivery-pass setting is unchanged). UI prefers the server `phase` and falls back to the client helper when the richer read model is unavailable; neither side persists a separate ChangeRequest status enum. Status counts (`GET /api/v1/status`, `get_governance_status`) report `delivered` as its own bucket, and delivered items are excluded from attention and from the ready/handed-off work queues.
- The ChangeRequest DTO carries a derived `tracker_status` field: the raw tracker `state.type` (`triage|backlog|unstarted|started|completed|canceled`, plus the terminal `removed` when the issue was deleted) of the most recent `delivery.tracker_status_changed` feedback event correlated to the CR (by the payload `correlation_id` matched against the CR id or key), or empty when none. Computed on read; never persisted.
- Closed stale-warning codes are `canonical_artifact_missing`, `canonical_artifact_unapproved`, `canonical_artifact_superseded`, `canonical_promotion_available`, `lead_artifact_superseded`, `linked_knowledge_newer`, `context_pack_stale`, `feature_deprecated`, `delivery_in_progress`, and `tracker_status_conflict`. The `delivery_in_progress` code signals an open MR/PR linked to the feature (derived from `integration_delivery_links.state = opened`); severity is `info` and it clears automatically when the MR merges or closes. The `tracker_status_conflict` code (severity `warning`) signals that the inbound tracker status contradicts the git/MCP merge evidence for the CR — tracker `completed` with no merged delivery, or a merged delivery while the tracker is neither `completed` nor `canceled`. It augments but never overrides the artifact-derived phase, and is emitted only on a clear contradiction (no tracker event = no warning).
- `linked_knowledge_newer` is emitted when the latest indexed Governance Knowledge linked to the feature id or feature key has an `updated_at` later than the feature's canonical artifact approval time. Feature Map should offer a New request handoff that asks the governance-ops to compare the linked knowledge against the canonical artifact and draft a feature update.

## Design reference contract

- Design references may include Figma URLs, selected frame IDs, exported snapshots, screenshots, or product mockups
- The Agent module may use design references to infer flows, states, copy, interactions, and missing edge cases
- If a design reference cannot be accessed or inspected, the Agent module must record the limitation as an assumption
- Approved artifacts should reference relevant design inputs through `design_refs` in the manifest

## Implementation feedback contract

- Implementation handoff starts only after a ChangeRequest has both an approved lead artifact and a Context Pack artifact. Context Packs are pasteable implementation briefs: they include the execution brief, ChangeRequest intent, acceptance criteria, approved/canonical artifact references, the full PRD and Spec (the implementation contract, not a preview), FE/BE/QA task sections, rollout/risk guardrails, a derived **Domain Vocabulary** section from glossary/language-reference files when present, a Scope & Blast Radius section (impacted services/apps and files likely touched, parsed from the manifest), Design References (Figma links parsed from the manifest), an **Applicable Skills** section (the profile snapshot's gate-bound rubric Skills, deduped and resolved to `specgate://skills/{id}` pointer rows so the coding agent pulls each Skill's current version; omitted when the profile binds none), and implementation guidance hydrated from the approved lead artifact or Feature canonical artifact.
- The Context Pack is the canonical implementation handoff artifact. Developers paste it into their local IDE or delivery tool; the governance-ops service does not clone repositories or start delivery tooling.
- Implementation kind is one of `new_feature`, `bug_fix`, or `change_request` and should be mirrored to GitLab labels: `specgate/type:new-feature`, `specgate/type:bug-fix`, or `specgate/type:change-request` when a downstream delivery tool creates GitLab artifacts.
- Secrets stay server-side and must not be returned in UI handoff payloads.

- Implementation agents must report spec issues discovered during code inspection or implementation
- Minor implementation mechanics may be adjusted without regenerating the spec when scope, contract, behavior, and acceptance criteria do not change
- Blocking issues must pause production-critical implementation and create feedback linked to `artifact_id`, `version`, and `run_id`
- Blocking feedback includes issue type, severity, affected files, evidence, and suggested correction
- Issues that change API, schema, data behavior, security, migration, dependency semantics, or acceptance criteria require Agent module regeneration and human approval before production-critical work resumes
- Spec feedback is a workflow/run record; it does not mutate an approved artifact in Doc Registry directly
- Implementation agents must not silently change approved product scope or technical contract

## Feature summary contract

A Feature Summary is an optional stored product-memory rollup for one Feature across its artifacts and work items (canonical file key `feature-summary.md`). It records current behavior, why the feature exists, the canonical artifact, linked work items, and known gaps. It is descriptive memory, not the implementation contract. It is set explicitly through the workboard API (`PUT /workboard/features/{id}/summary`), and a `feature_summary_outdated` warning flags when it falls behind the current canonical version. There is no autonomous generation.

## Coding agent handoff contract

Coding agents consume SpecGate through two layers:

1. `specgate://context-pack/{change_request_id}` — the canonical MCP resource containing the durable implementation brief; coding agents read the same pack through `specgate work context`.
2. Tool setup — the `specgate` CLI plus native plugin manifests and focused Skills/hooks installed by the `curl | sh` installer; Cursor additionally receives its rules file. The Skills carry the behavioral workflow for reading Context Packs and reporting feedback.

The Context Pack remains the source of truth for what to implement. Agent instructions are behavioral guidance and must not introduce new product scope.

The product must optimize for low-friction internal setup. Handoff UI should show the deployment installer for CLI + IDE Skills/hooks/rules, and native plugin install commands where supported by the target tool.

Coding agents should share the same artifact-governance loop as governance-ops where possible:

- `artifact_create` may publish a new artifact bundle when the task is explicitly creating a new artifact.
- `draft_artifact_update` may open a draft-only artifact-update proposal session for an existing artifact.
- Neither tool may auto-approve, auto-save, or mutate an approved artifact in place.

### Coding-agent feedback event types

- `coding_agent.blocked_ambiguity` — implementation is blocked because the approved artifact is ambiguous, wrong, or incomplete.
- `coding_agent.completed` — the coding agent believes implementation is complete and reports evidence.
- `coding_agent.docs_updated` — repo-side system/spec docs were updated as part of implementation.
- `delivery.comment_scope_drift` — a git/tracker comment suggests scope, acceptance criteria, security, data behavior, migration, or contract drift.

All feedback events are append-only `governance_feedback_events` rows. Feedback never mutates approved artifacts directly. A feedback row persists with status `pending` (surfaced as `received`) and is reconciled to `processed` or `ignored` when a proposal that references it is approved or rejected. Feedback does not automatically create a proposal; a proposal may be opened on demand tagged with `source_kind=feedback_event` and `source_id=<governance_feedback_events.id>`, and its verdict then reconciles the event.

Feedback ingestion should deduplicate by `provider + external_id + event_type + change_request_id` when those fields are available. Coding-agent feedback should use `run_id + event_type + change_request_id` as the first dedupe key and fall back to a stable hash of summary/evidence.

### MCP work discovery and readbacks

- `resolve_work_item(provider, issue_key?, issue_url?)` maps a tracker issue back to the SpecGate work item and Context Pack URI, including lane-scoped URIs for split FE/BE tracker issues.
- `list_work_items(ready?, handed_off?, work_type?, workspace_id?, mine?, limit?)` lists ready or handed-off work items for IDE-native pickup. `workspace_id` narrows results to locally attributed work items in one workspace and is a selection filter, not authorization. `mine=true` is reserved for identity-aware filtering and currently returns a validation error.
- `read_delivery_review(change_request_id)` returns the latest `delivery_review` verdict, per-criterion review detail, automated checks, and `outstanding_md` for failed reviews.
- `read_clarification(change_request_id, since?)` lets a blocked coding agent poll for the human outcome of `coding_agent.blocked_ambiguity` feedback. It returns only terminal outcomes (`accepted` or `rejected`) and omits pending reports; `answer_md` is the reviewed reason recorded when the feedback is reconciled.

### `report_implementation_feedback` MCP tool

Input:

```json
{
  "change_request_id": "cr_123",
  "event_type": "coding_agent.blocked_ambiguity",
  "severity": "blocking",
  "summary": "Acceptance criterion does not define refund behavior for partial returns.",
  "evidence": [
    {"kind": "file", "path": "services/orders/refunds.go", "line": 42},
    {"kind": "artifact_section", "file_key": "spec", "heading": "Refund lifecycle"}
  ],
  "suggested_correction": "Clarify whether partial returns reverse loyalty points proportionally.",
  "affected_files": ["services/orders/refunds.go"],
  "agent": {"name": "cursor", "version": "unknown"},
  "run_id": "optional-agent-run-id",
  "dedupe_key": "optional-stable-client-key"
}
```

On `coding_agent.completed`, also send the **completion-evidence** fields (optional, backward-compatible — they ride `payload_json`, no migration):

```json
{
  "event_type": "coding_agent.completed",
  "summary": "Implemented redeem/unredeem with idempotent hold-not-burn ledger.",
  "checks": [
    {"name": "tests", "status": "pass"},
    {"name": "lint", "status": "fail", "detail": "2 issues"}
  ],
  "criteria": [
    {"criterion_id": "ac_1", "claim": "satisfied", "evidence": {"kind": "file", "path": "checkout/redeem.go"}},
    {"text": "Show connect-supplier CTA", "claim": "not_done"}
  ]
}
```

- `checks[]` = `{name: tests|types|lint|build, status: pass|fail|skipped, detail?}`.
- `criteria[]` = `{criterion_id? (correlates to the work item's acceptance-criterion id), text?, claim: satisfied|partial|not_done, evidence?}` where `evidence` follows the coding-agent evidence shape `{kind, path?, line?, file_key?, heading?, url?}`.
- These are what the **delivery reviewer** (`delivery_review` gate) verifies each acceptance criterion against → a Pass/Fail verdict. **Overall pass only if every criterion is met AND no check failed.** Omitting them still succeeds; the review degrades to judging against `summary` and marks thin evidence `unclear`.
- The verdict is persisted as a `delivery_review` **GateRun** (agents runner → `POST /workboard/change-requests/{id}/review-delivery` → the existing gate-runs/refresh endpoint; eval-only gate, no migration) and rendered on the work item's Verification surface (a latest passing `delivery_review` run also surfaces the derived `Delivered` board phase until the item is archived). On **fail**, the on-read Context Pack folds the unmet criteria + failing checks into an **"Outstanding Review Feedback"** section so a re-handoff carries the gaps to the next build pass (Governance → Builder → Quality Gates → Reviewer → Pass/Fail).
- The **`spec_completeness`** gate is the **readiness check**: it locates the **minimum-executable-contract** (goal, scope, non-goals, acceptance criteria, constraints, risks, verification) plus build-readiness depth across the artifact's documents **resolved by role** (`GET /artifacts/{id}/files` → by-role bundle), so it works over any spec format. Advisory: any missing/partial required topic ⇒ `warn`, never `fail`. It persists like the other eval-only gates; its `evidence_json.evidence` carries `{ "topics": [{topic, status, why, where}], "summary" }` where `status ∈ covered | partial | missing | not_applicable`. The UI renders it as the Build-readiness checklist; its summary rides the Context Pack as an "Unresolved Quality Gates" line when `warn`. Profile-specific required sets are per change-type (SP1).

Rules:

- `severity=blocking` requires human review before production-critical implementation continues.
- Minor mechanics that do not change scope, contract, behavior, data, security, migration, dependencies, or acceptance criteria should not create blocking feedback.
- The MCP tool returns the created feedback event id and whether an artifact-update proposal should be drafted.
- Repeated reports with the same dedupe key update or return the existing feedback event instead of creating a duplicate.

### `draft_artifact_update` MCP tool

Input:

```json
{
  "artifact_id": "art_123",
  "change_request_id": "cr_123",
  "summary": "Delivered behavior differs from the approved spec for partial refunds.",
  "files": {
    "spec": "# Spec\n\nUpdated full markdown here"
  },
  "requested_by": "cursor",
  "dedupe_key": "optional-stable-client-key"
}
```

Rules:

- The tool opens a sourced `artifact_edit_session` with `source_kind=coding_agent_update`.
- `files` contains full replacement markdown for existing artifact file keys only.
- The tool is draft-only: it never saves, approves, or publishes automatically.
- Unchanged files are ignored; if no valid changed files remain, the tool must fail.
- Repeated calls with the same dedupe key return the existing active proposal instead of opening a duplicate.

## Artifact-update proposal contract (reconciliation)

Closes the spine's right half: a signal (a delivery/feedback event, a failing gate) can be turned into a **reviewed** artifact-update proposal that a human approves or rejects — feedback never rewrites an approved artifact directly.

- A **proposal is an artifact edit session** tagged with its origin: `source_kind` (`feedback_event`, `coding_agent_update`, `gate`, …) + `source_id`. Ordinary edit sessions have empty source.
- A proposal's **content (the proposed hunks) is supplied when the session is opened** — a reviewer's requested changes, a manual edit, or an agent calling `draft_artifact_update` with the changed files. Doc Registry owns the **queue and verdict**, not the authoring of the hunks.
- `GET /artifact-edit/proposals` is the **review queue** — sourced sessions still `active`.
- **Verdict reuses existing session verbs:** approve = save (creates a draft revision; `artifact_id` empty until a separate materialization step — never a canonical/approved-artifact mutation); reject = discard.
- **Re-applying onto a moved base creates an already-resolved session in one request:** `POST /artifact-edit/sessions` accepts optional `working_files: [{key, content}]` that seed the working files atomically at creation, so there is no create-then-write window in which an abandoned re-apply orphans a sourced session.
- **On verdict, a `source_kind=feedback_event` proposal reconciles its originating signal:** approve marks the linked feedback row `processed`, reject marks it `ignored` (best-effort, cross-store).

## Dependency contract

- Dependencies are declared with `feature_id`, `min_version`, and `status_required`
- Production-critical workflows must block if a dependency is missing or below status threshold

## Tracker handoff contract (adapter branch)

Trackers (Linear first, then Jira / GitHub Issues) are an **optional
upgrade** on top of the git + MCP floor — never required for handoff, and SpecGate
never persists tracker execution status as its own field. This contract defines
the adapter seam; inbound webhook normalization is wired for Linear + GitLab
(see "Inbound tracker-webhook normalization" below), while multi-source
reconciliation remains deferred. SpecGate does not create tracker issues
outbound: issues are authored in the tracker (manually or by other tools) and
correlated inbound.

Linear is the reference adapter: it exposes a hosted **MCP server**
(`https://mcp.linear.app/mcp`) and a typed GraphQL/webhook API
(<https://linear.app/developers>), so the same MCP handoff floor SpecGate already
speaks doubles as the Linear integration surface.

### Correlation-id convention

The correlation is carried **two ways** so it survives any tracker:

1. A native custom field `specgate_correlation_id` when the tracker has custom fields
   (e.g. Jira). Linear has no generic per-issue custom fields, so the footer
   below is the carrier there — the native-field path is a best-effort upgrade,
   never the sole anchor.
2. A `fixes SPECGATE-{key|id}` footer in the body/description — the
   universal-floor footer the inbound git/tracker webhook parser reads, so any
   issue that carries it correlates to the original work item. Reference fixture
   (Linear webhook shape): `app/doc-registry/internal/integrations/testdata/tracker/`.

### Inbound tracker-webhook normalization

`POST /integrations/{id}/linear/webhook` receives a Linear webhook. Auth is the
`Linear-Signature` header — a bare-hex HMAC-SHA256 of the raw body (no
`sha256=` prefix), verified against the server-configured `LINEAR_WEBHOOK_SECRET`
env secret (Linear-managed; an unset secret rejects inbound webhooks with 401). On
a valid `Issue` event the handler:

- resolves the work item by the persisted `tracker_links` row the handoff wrote
  (matched on the issue's immutable `data.id` / `identifier`), falling back to the
  `fixes SPECGATE-{key|id}` footer in `data.description` — so correlation survives a
  description edit that drops the footer,
- carries the Linear `data.state.type` (`triage` / `backlog` / `unstarted` /
  `started` / `completed` / `canceled`) **raw** — it does not map onto the
  MR-shaped delivery states (`opened` / `merged` / `closed`),
- emits a `delivery.tracker_status_changed` feedback event **only on a real state
  transition** (a same-state re-delivery — e.g. a title/description edit — is
  ignored with `tracker_status_unchanged`), carrying the Linear `identifier`
  (e.g. `LOY-128`), the raw `tracker_state`, the issue url/title, the resolved
  work-item id, and the correlation key,
- on a top-level webhook `action: "remove"` (issue deleted) emits a terminal
  `tracker_state="removed"` and marks the link removed.

A handoff persists one `tracker_links` row per lane (keyed `integration_id` +
`external_key`); these back the work-item "linked issues" surface
(`GET /workboard/change-requests/{id}/tracker-links`) and the link-first
inbound correlation above. `delivery.tracker_status_changed` is a registered
entry in the shared feedback vocabulary the UI/agents consume, alongside the
`delivery.pr_*` git signals; its `tracker_state` now includes the terminal
`removed`. Trackers stay optional: emission does not require a matched work item
and never gates the git/MCP floor. Inbound deliveries dedup on the Linear
`webhookId`.

**Per-integration webhook secrets (GitLab + GitHub).** Unlike Linear, GitLab and
GitHub inbound webhooks are verified against a **per-integration** secret stored
encrypted (`webhook_secret_encrypted`), not an env secret — read/set via
`GET/PUT /integrations/{id}/webhook-secret` and `POST …/rotate`:

- **GitLab** is validate-only and follows the **Standard Webhooks** spec. GitLab
  generates a `whsec_…` **signing token** (the user pastes it via `PUT`); each
  delivery carries `webhook-id` / `webhook-timestamp` / `webhook-signature`, and
  the handler verifies HMAC-SHA256 over `{webhook-id}.{webhook-timestamp}.{body}`
  (base64, `v1,` prefix, match any of the space-separated signatures) plus a
  timestamp-recency check. No generation/rotation on our side.
- **GitHub** signs with `X-Hub-Signature-256` over a secret Doc Registry
  **generates** (get-or-create on `GET`, regenerate on rotate); the user copies it
  into GitHub's webhook *Secret* field.

An unset secret rejects inbound webhooks with 401 (no open relay).

**GitLab as a tracker (inbound).** A GitLab `Issue Hook` (`object_kind=issue`)
arrives on the *same* `POST /integrations/{id}/gitlab/webhook` endpoint as the MR
events and is handled before the MR filter: same signing-token auth, then it
emits the same `delivery.tracker_status_changed` signal (identifier `#{iid}`,
issue url/title, `fixes SPECGATE-{key}` correlation). GitLab issues have no rich
workflow state, so `tracker_state` derives from a scoped `workflow::` label
(`done`/`completed` → `completed`, `doing`/`started` → `started`) falling back to
the issue open/closed state (`closed` → `completed`, else `started`) — mapped onto
the same provider-neutral state vocabulary as Linear so the conflict-warning
logic stays provider-agnostic.

### Tracker adapters

The per-provider tracker adapters are registered for inbound webhook
normalization and the delivery-pass auto-transition (closing linked issues).
`tracker_links` rows are maintained by the inbound webhook path.

**GitHub tracker integration.** Connected GitHub integrations provide tracker
metadata and webhook handling through Doc Registry integration resources. Repo
reading for coding agents stays IDE-side; Doc Registry does not expose an
outbound external-MCP-server registry for GitHub or generic MCP tools.

Trackers stay optional: the tracker never gates the git/MCP floor, and SpecGate
does not persist tracker execution status as its own field. The CR↔issue link
is carried by `tracker_links` rows and the `fixes SPECGATE-{key}` footer, which
the inbound webhook parser reads to correlate returning status changes.

### Deferred (not built in the thin slice)

- **Multi-source reconciliation** — tracker status augments the derived phase but
  must not override higher-confidence git/MCP evidence without a conflict
  warning. This is full-phase work, not the thin slice.

## Governance publish contract

These fields extend the existing `POST /artifacts` request body (see `app/doc-registry/docs/spec.md` §6.2). All existing fields (`request_type`, `impact_level`, `feature_id`, etc.) remain unchanged.

### New publish request fields

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

**`requested_governance_level`** (optional string): the caller's preferred governance level (`light` | `standard` | `enhanced`). Policy may override this upward — the resolved level is stored in `governance_level` inside the snapshot and may differ.

**`impact_declaration`** (optional object): caller-supplied impact signals used by the policy engine to resolve the governance level. Missing object or missing individual fields normalize to `"unknown"` at resolution time.

#### `impact_declaration` fields

| Field | Type | Values | Description |
| --- | --- | --- | --- |
| `protected_domains` | array of string | e.g. `["payment", "auth"]` | Named protected domains this change touches. |
| `protected_domains_status` | tri-state | `yes` \| `no` \| `unknown` | Whether the change affects any protected domain. `unknown` is the only field that triggers `criticalUnknown()` — forcing `governance_level` to `enhanced` regardless of other fields. |
| `data_or_schema_change` | tri-state | `yes` \| `no` \| `unknown` | Change modifies a database schema, stored data format, or migration. |
| `external_contract_change` | tri-state | `yes` \| `no` \| `unknown` | Change modifies a public or cross-service API contract. |
| `irreversible_or_complex_rollback` | tri-state | `yes` \| `no` \| `unknown` | Rollback requires multi-step coordination or is not fully reversible. |
| `broad_blast_radius` | tri-state | `yes` \| `no` \| `unknown` | Change affects a large surface area across multiple services or teams. |
| `affected_systems` | array of string | e.g. `["checkout-api"]` | Informational list of system names affected. Not used in policy resolution. |
| `expected_paths` | array of string | glob patterns | Informational expected file paths. Not used in policy resolution. |

**Tri-state semantics:** `yes` = caller confirms the condition holds; `no` = caller confirms it does not hold; `unknown` = caller cannot determine. Missing fields are treated as `unknown`. Only `protected_domains_status == "unknown"` triggers fail-safe escalation to `enhanced`.

### Resolved governance snapshot (v1)

The `gates_profile_snapshot_json` field on a stored artifact carries an immutable resolved governance snapshot. Policy v1 snapshots use `snapshot_schema_version: "specgate.policy/v1"` and add the following fields on top of the pre-existing execution projection (`required_roles`, `required_topics`, `required_evidence`, `enabled_gates`, `renderer_key`, `approval_policy`, `evidence_policy`, `gate_skills`, `digest`):

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

**New fields in v1:**

| Field | Type | Description |
| --- | --- | --- |
| `snapshot_schema_version` | string | Always `"specgate.policy/v1"` for policy v1 snapshots. Used by consumers to detect the schema generation. |
| `work_type` | string | Normalized work type (`bugfix` \| `change_request` \| `new_feature`). Replaces the older `change_type` terminology in new snapshots. |
| `risk_level` | string | Resolved risk level (`low` \| `medium` \| `high`). Derived from `impact_level` and `impact_declaration`. |
| `requested_governance_level` | string \| null | The caller's preferred level from the publish request; may have been overridden upward by policy. Null when not supplied. |
| `governance_level` | string | The policy-resolved governance level (`light` \| `standard` \| `enhanced`). This is the enforced level, not the requested one. |
| `reason_codes` | array of string | Machine-readable codes explaining why this governance level was assigned. Values: `low_risk_bugfix`, `high_impact`, `complex_rollback`, `broad_blast_radius`, `protected_domain`, `unknown_protected_domain`, `default_standard`. |
| `policy_lineage` | array of `{key, version}` | Ordered list of built-in policy entries applied during resolution. Consumers can use this to detect if the active policy has changed since the snapshot was created. |

**Conformance fixtures** for the policy resolution engine live at `docs/conformance/governance-policy-v1/resolution-cases.json` (5 cases, `schema_version: "specgate.policy/v1"`).

## Gate contracts

Gate tasks and results flow through the Doc Registry REST API for IDE agents and custom policy implementations.

### GateTask

Returned by `GET /api/v1/gate-tasks/{task_id}`. Represents a gate evaluation task assigned to an executor.

```json
{
  "task_id": "uuid",
  "gate_key": "namespace/name@version",
  "gate_version": "string",
  "gate_digest": "sha256:<hex>",
  "artifact_id": "uuid",
  "artifact_digest": "sha256:<hex>",
  "profile_digest": "sha256:<hex>",
  "executor": "ide_agent | platform_llm | human | deterministic | external",
  "skill_content": "string (evaluation rubric markdown, present when executor=ide_agent)",
  "expires_at": "RFC3339 timestamp"
}
```

**Fields:**

| Field | Type | Description |
| --- | --- | --- |
| `task_id` | uuid | Unique task identifier |
| `gate_key` | string | Qualified gate name: `namespace/name@version` |
| `gate_version` | string | Version of the gate definition |
| `gate_digest` | sha256 | Hash of the gate definition; must match result `gate_digest` exactly |
| `artifact_id` | uuid | ID of the artifact being evaluated |
| `artifact_digest` | sha256 | Hash of the artifact; must match result `input_digest` exactly |
| `profile_digest` | sha256 | Hash of the resolved governance profile snapshot |
| `executor` | enum | The assigned executor role |
| `skill_content` | string | IDE agent evaluation rubric (markdown); present only when `executor=ide_agent` |
| `expires_at` | RFC3339 | Expiration timestamp for task validity |

### GateResult

Submitted by `POST /api/v1/gate-tasks/{task_id}/result`. Captures the evaluation outcome.

```json
{
  "gate": "namespace/name@version",
  "gate_digest": "sha256:<hex> (must match task.gate_digest exactly)",
  "input_digest": "sha256:<hex> (must match task.artifact_digest)",
  "state": "pass | warn | fail | needs_human_review | not_applicable | not_run",
  "summary": "string (optional)",
  "evaluator": {
    "executor": "ide_agent | platform_llm | human | deterministic | external",
    "name": "string (optional, agent name)",
    "run_id": "string (optional, trace/run identifier)"
  },
  "findings": [
    {
      "severity": "critical | major | minor | info",
      "code": "string",
      "message": "string",
      "location": "string (optional)"
    }
  ]
}
```

**Fields:**

| Field | Type | Description |
| --- | --- | --- |
| `gate` | string | Qualified gate key matching the task |
| `gate_digest` | sha256 | **Must match** `task.gate_digest` exactly; stale digest → 422 Unprocessable Entity |
| `input_digest` | sha256 | **Must match** `task.artifact_digest` exactly |
| `state` | enum | Evaluation outcome; values: `pass`, `warn`, `fail`, `needs_human_review`, `not_applicable`, `not_run` |
| `summary` | string | Human-readable summary of the evaluation (optional) |
| `evaluator` | object | Executor metadata |
| `evaluator.executor` | enum | Must match or be compatible with assigned executor |
| `evaluator.name` | string | Optional: name of the evaluator agent or system |
| `evaluator.run_id` | string | Optional: trace/run identifier for audit trail |
| `findings` | array | Optional: detailed findings with severity levels |
| `findings[].severity` | enum | `critical`, `major`, `minor`, `info` |
| `findings[].code` | string | Machine-readable finding code |
| `findings[].message` | string | Human-readable finding description |
| `findings[].location` | string | Optional: file path, line number, or location within artifact |

### GateResult response

Returned after successful submission (HTTP 200).

```json
{
  "result_id": "uuid",
  "trust": "agent_attested | platform_evaluated | human_decision",
  "state": "pass | warn | fail | needs_human_review | not_applicable | not_run"
}
```

**Fields:**

| Field | Type | Description |
| --- | --- | --- |
| `result_id` | uuid | Unique identifier for the stored result |
| `trust` | enum | Trust classification based on executor and validation |
| `state` | enum | The evaluation outcome (echoed from request) |

### Trust stamping rules

Trust classification is deterministic based on the executor and validation:

- `executor=ide_agent` → `trust=agent_attested` — result is trusted as coming from a verified IDE agent
- `executor=platform_llm` → `trust=platform_evaluated` — result is evaluated by the platform's own LLM
- `executor=human` → `trust=human_decision` — result is a human decision
- `executor=deterministic` → deterministic rules, no promotion
- `executor=external` → external system result

**Promotion is never allowed:** a result stamped `agent_attested` cannot be re-submitted or re-stamped as `platform_evaluated` or `human_decision`. Each executor path is terminal.

### Validation rules

- **Stale gate definition:** if `gate_digest` does not match `task.gate_digest`, reject with HTTP 422 Unprocessable Entity and error message containing `"stale"`
- **Artifact mismatch:** `input_digest` field is accepted but not currently validated.
- **Executor mismatch:** if `evaluator.executor` does not match the assigned task executor (or is not in the `allowed` list), reject with HTTP 400
- **Missing digest fields:** `gate_digest` is required; omission → HTTP 400. `input_digest` is optional.

### Conformance fixtures

Conformance fixtures for gate result validation live at:

- `docs/conformance/governance-policy-v1/gate-result-validation-cases.json` — 3 test cases for gate result acceptance and trust stamping

## Naming rules

- Use snake_case in API payloads
- Use kebab-case for feature IDs
- Use lowercase enum values
- Keep module names short and descriptive
