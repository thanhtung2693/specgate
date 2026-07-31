# Contracts

Use this reference when a change crosses module boundaries. It records the
vocabulary and behavior that must match between the SpecGate CLI, Doc Registry,
agents service, UI, IDE plugins, and integrations. Module-specific details stay
in the module specifications.

For product workflow, use [How SpecGate works](../using-specgate/concepts/how-specgate-works.md)
and the [CLI reference](../using-specgate/reference/cli.md). For the Doc Registry
route catalog, see
[app/doc-registry/docs/spec.md](../../app/doc-registry/docs/spec.md).

## Module Boundaries

| Module | Owns | Does not own |
| --- | --- | --- |
| CLI | install, local config, workspace selection, work/artifact/delivery commands, IDE plugin install | model reasoning or durable state |
| Doc Registry | artifacts, workboard, Knowledge, settings, evidence, integrations, REST, persistence | LLM judgment |
| Agents | model-backed readiness, delivery review, governance chat | direct durable writes outside Doc Registry APIs |
| UI | review, inspection, settings, team/workspace views, governance chat shell | hidden sample data or source-of-truth mutation outside APIs |
| IDE plugins | skills/hooks/rules that guide a coding agent through the CLI | product scope or approval authority |

Doc Registry is internal-open behind a trusted network: it has no authentication
of its own, and an appliance with no member credentials accepts any caller the
gateway lets through. Where credentials exist the gateway authenticates and
forwards the verified identity, so attribution stops being cooperative — see
Actor precedence below. There is still no RBAC anywhere: an authenticated caller
may do anything the API allows.

## HTTP Surfaces

Doc Registry serves two HTTP dialects:

- `/api/v1/*` is the stable CLI facade. It covers meta/status, work items,
  delivery, artifacts, gate tasks, skills, users, workspaces, policies,
  Knowledge, and model/setup checks.
- Unversioned routes are the app/service surface used by the UI and agents:
  `/workboard/*`, `/features`, `/documents`,
  `/integrations`, `/governance/*`, `/settings`, and artifact detail routes.

New user-facing commands should prefer `/api/v1`. UI-only workflows may use the
unversioned surface because UI and server ship together.

`GET /api/v1/meta` exposes typed capability details. `governance_chat` is
derived from the Agents chat-health route: a reachable service without its
support-model key is `configuration_required`, an absent or unreachable route
is `unavailable`, and only configured chat is `available`. Generic Agents
service presence must not be used as a substitute.

## Identity and Workspace

Users, workspaces, and workspace members support attribution and filtering.
They do not grant access.

- CLI user and workspace selection live in local user config.
- `specgate init` binds its current Git checkout. Other checkouts can be bound
  with `specgate workspace bind`; bindings remain local and are not committed.
- Workspace-scoped CLI operations in a Git checkout must resolve through an
  explicit override, project binding, or repo default. They fail before Local
  storage or Full API access when only a global fallback exists.
- Repo `.specgate/config` may carry shared server/workspace defaults; user
  identity must stay outside the repo.
- Identity bootstrap idempotently installs missing built-in rubric Skills in
  the selected workspace before returning, so its first artifact publication
  can freeze the complete automatic policy without a server restart.
- `GET /api/v1/workspaces/{id}/members` is read-only. It returns the workspace,
  optional current-user match, and member rows. `current: true` means the
  supplied local identity matched a member row; `not_member` is a status signal,
  not an authorization denial.

## Work and Artifacts

### Route

A work item is **quick route** when it has no lead artifact, and
**artifact-backed** when it has one. That is the whole rule, and every module must
derive it from that fact alone: `ChangeRequest.IsQuickRoute`, the Context Pack
builder, delivery-review policy resolution, and the UI's work-item mapper.

Work type does not decide the route. It used to appear in the condition
(`no lead artifact AND work_type == bug_fix`), which broke both directions: a
quick item of any other type fell through to the artifact-backed path, where gates
it could never satisfy applied and delivery review blocked it for missing a policy
snapshot it could not have; meanwhile the UI derived the route from work type
alone, so artifact-backed cleanup read as quick. Quick creation still stores a
placeholder work type because the enum has no "unspecified" member; nothing routes
on it and no surface displays it.

Board **phase** is a separate question and still uses work type as a proxy for
"quick work that has an approved contract". Attempting to remove that proxy found
why it cannot be removed yet, and the reason is a storage gap rather than a
missing query.

`Intake` means the work has not been approved into a handoff: no pinned artifact,
and no approved contract of its own. `Ready` means one of those exists. Quick-route
work is Ready because the human approves its criteria at creation. The demo's
Intake example has drafted criteria and no pinned artifact, and its feature has an
approved canonical artifact, so no fact about the feature separates the two cases
either.

What separates them is whether the criteria were **approved** or merely drafted.
Storage accepts a `source` of `human` or `llm` per criterion, and the CLI now sends
`human` for every criterion it persists, because both `work create-quick` and
`change approve` require the human to supply them and the installed skills must not
create the record until the displayed contract is approved. Criteria arriving
without a source still default to `llm`, which is correct for a draft.

Every writer now sets it: the CLI sends `human` for criteria it persists, and the
governance-chat quick-work path sends `human` for criteria the caller supplied and
`llm` for criteria the model drafted, which is the distinction that path actually
has. A criterion arriving without a source still defaults to `llm`.

Board phase still keeps the work-type proxy. `source` is now truthful enough to
derive readiness from, but changing what Intake and Ready mean is a behaviour
change with its own tests and demo-data expectations, so it is a separate change
rather than a rider on this one.

### Mode-aware handoff

For an artifact-backed work item, the IDE agent reads every explicitly mapped
source spec document and submits each confirmed criterion with a repeated
`work create --ac`. SpecGate does not infer criteria from headings, numbering,
filenames, or keywords. The persisted work-item criteria are then the canonical
set for completion and peer-review coverage.

`POST /api/v1/work-items/create` also accepts optional structured
`source_refs[]`. Portable Local-to-Full import uses a source-workspace-qualified
reference here so exact retries can identify the same work item without title
or prose matching. That trusted import path may also pass `artifact_id` to
preserve the work item's exact approved artifact when a newer version is
canonical; the server verifies the artifact's feature, workspace, and status.

Full mode may return a server-advertised URL through `specgate open --print`.
Local CLI mode has no browser UI or server: a handoff names the human-readable
work/artifact title and stable ID, then gives the exact CLI action such as
`specgate delivery approve <work-ref>`. Agents must never invent a localhost
URL for either mode.

The product hierarchy is:

```text
Workspace
  -> Feature
     -> ChangeRequest / work item
        -> lead artifact
        -> Context Pack
        -> delivery evidence
```

Feature, Artifact, and ChangeRequest rows are workspace-owned governed roots.
Human-facing Feature keys are unique within a workspace, not globally. Normal
list/detail/mutation calls carry a non-empty selected workspace. Missing
workspace context or a workspace ID that is not one safe opaque path segment is
rejected with `400`; a known ID from another workspace returns the same
not-found shape as an unknown ID. Artifact publish validates that any linked
Feature belongs to the same workspace before writing.

This requirement applies to normal WorkBoard product endpoints, including
Feature/ChangeRequest lists, details, mutations, acceptance criteria, gate
runs, next actions, and stale warnings. Provider callbacks derive ownership
from their named integration rather than accepting a caller-supplied workspace.

Gate tasks and unified gate runs follow the same boundary. Gate-task list,
detail, dispatch, and result routes require `workspace_id`; task records and
artifact-scoped run rows store the owning workspace. ChangeRequest gate runs
derive ownership from the ChangeRequest. A task or run from another workspace
is not returned, and writes without a resolved owner fail. Lifecycle audit rows
also store the owning workspace and are read only within it.

Dispatch atomically creates at most one current task for the same workspace,
artifact/input digest, gate key, and gate digest. Each task carries the frozen
gate version and rubric. Result submission must match the task's gate key,
input digest, gate digest, workspace, and executor.

Integration root catalog and lifecycle calls (`GET/POST /integrations` and
`GET/PUT/DELETE /integrations/{id}`) also require a non-empty workspace. Child
provider/resource routes derive the same boundary from their integration.
Feedback events store their own required workspace owner, including
agent-originated feedback without an integration.

Artifact identity is `workspace_id + feature_id + version`; versions use
`vMAJOR.MINOR`.
Artifact statuses are:

- `draft`
- `approved`
- `needs_changes`
- `superseded`

Approved artifacts are immutable. IDE/CLI agents publish a new artifact version
instead of mutating an existing body. Humans review, approve, reject, request
changes, and inspect history; they do not author artifact bodies through the
registry.

Governance chat follows the same boundary. The UI creates one LangGraph thread
for the active workspace and tags it with `workspace_id`; it exposes no thread
history or management surface, and Doc Registry stores none of this chat state.
Each run also receives `workspace_id` and `thread_workspace_id` through trusted
runtime configuration; the model cannot choose either value. Switching
workspaces remounts the UI runtime, so new runs use the new workspace while an
existing run remains pinned to its original thread context.

Governance Skills are workspace-owned settings. Skill catalog and mutation
calls carry the selected `workspace_id`; a Skill from another workspace is
treated as not found. Automatic governance policy itself is global and code
defined; workspaces customize gate behavior by changing the named Skills that
the automatic policy binds.

Custom Skills follow the same boundary: list/get/create/update/delete operations
require the selected workspace, and duplicate names are allowed only across
different workspaces.

Feature attachments are workspace-owned children. Their Feature and optional
Governance File parent must resolve inside the same workspace; create/list/delete
operations require `workspace_id`. Context Pack attachment reads pass the CR
workspace explicitly.

Persisted Knowledge citation provenance is server-normalized (canonical location,
title, authority, staleness, document/version, and span); callers cannot persist
arbitrary source labels or URLs. Stale and low-authority evidence warns but does
not invalidate a citation. A later effective changed file without retained
citation blocks save.

Artifact packages are flexible documents inside a fixed governance envelope. The
document paths are open; the role vocabulary is fixed:

| Role | Meaning |
| --- | --- |
| `spec` | governed source of truth: intent, scope, acceptance criteria |
| `design` | architecture, data model, implementation shape |
| `plan` | task breakdown |
| `verification` | test plan and evidence |
| `research` | background and decisions |
| `reference` | supporting non-normative material |
| `custom:*` | caller-specific role |

Product briefs, requirements, and PRDs normally use `spec`; callers needing a
separate label may use `custom:prd`.

Selected local packages are framework-agnostic at this boundary. Callers map
explicit files into the same `{path, role, bytes, provenance}` package; no
framework detection affects storage. A published version stores exact
normalized repository-relative paths, server-computed file SHA-256 values, a
deterministic manifest digest, and versioned source provenance. Local existence
or framework generation is not cloud synchronization; only successful publish
makes the snapshot available elsewhere.

The CLI source locator is explicit and never changes the artifact `path`:
`repo_file` resolves from the nearest Git root, `source_file` resolves inside
the manifest directory, `file_url` names an explicitly selected external local
file, and `content` is inline. Exactly one is accepted. The CLI replaces local
locators with UTF-8 content before crossing the service boundary and rejects
filesystem escapes or unsafe file types.

## Governance Policy

Publish resolves automatic governance policy and snapshots it onto the artifact.
Later readiness, handoff, and delivery review use the frozen snapshot, not any
mutable workspace setting. The CLI uses REST.

Integration catalog and lifecycle requests carry the selected `workspace_id`;
same-provider names may repeat across workspaces, while a known integration id
from another workspace is returned as not found. All normal root and child
integration routes require workspace context. Provider callbacks resolve the
named integration as the trusted parent and do not accept model/user workspace
input. Child resources, OAuth state, webhook events, delivery links, tracker
links, and credentials derive the same boundary from that parent; a scoped
request cannot read or mutate a child owned by another workspace. Feedback
without an integration still stores the trusted workspace supplied by its agent
caller.

Governance file rows are workspace-owned when created through a selected
workspace. File catalog, commit, touch, delete, and content-proxy requests use
the same `workspace_id`; a file from another workspace is indistinguishable from
an unknown ID. Development data with a NULL owner is rejected at write time;
there is no unscoped user-facing fallback.

The registry has no authentication of its own. The appliance gateway
authenticates callers when members hold credentials and forwards the verified
username in `X-SpecGate-User`, which the gateway sets on every proxying route and
never accepts from a client; the registry verifies one Basic credential at
`/internal/auth` for that purpose. With no member credentials configured the
gateway passes every request through, which is the default posture.

**Actor precedence.** Where the forwarded identity is present it *is* the actor on
a recorded decision, and `approved_by` / `decided_by` in the request body are
ignored. Those body fields remain authoritative only for a deployment with no
gateway credentials. Any new endpoint that records who decided something must
follow this precedence; see
[the gateway identity ADR](adr/2026-07-29-gateway-asserted-identity.md).

**Access changes are recorded.** Issuing, rotating, or revoking a member's gateway
credential appends an `identity.credential_issued` or
`identity.credential_revoked` row naming the member, the authenticated actor, and
the workspace the operator acted in. A change that cannot be recorded fails the
request, because access granted with no trace of it is the gap the record exists
to close. The first credential on an open appliance is issued by an
unauthenticated caller by necessity; that row carries an empty actor and says so
in its detail rather than attributing the change to nobody.

The trail is append-only by convention and carries **no hash chain**, unlike
`artifact_events`. It shows what the service recorded; it does not show that the
database was never edited afterwards, and no surface may describe it as
tamper-evident. `workspace members` reads back each member's credential state and
its most recent change.

Artifact submission source fields remain provenance, not cryptographic caller
identity, and a credential identifies a credential rather than a person: one
lent to a teammate or to an agent records its owner's name for their decisions.

Policy snapshots use `specgate.policy/v1` and include:

- work type and risk level;
- requested and resolved governance level;
- reason codes;
- required roles, topics, and evidence;
- enabled gates, gate-bound Skill names, and frozen gate definitions containing
  gate version plus exact Skill rubric content/digest;
- approval and evidence policy;
- policy lineage and digest.

Artifact document roles are caller-selected routing labels. Built-in Full
governance requires `spec` for Light and `spec` plus `plan` for Standard and
Enhanced; it does not require a separate file for every concern.
`verification` and `design` remain valid optional roles. Verification methods
may live in the spec, plan, or optional verification material, while delivery
still requires criterion-specific claims and observed check evidence. Gate
inputs are selected by explicit role only; paths, filenames, headings, and
framework names never infer a role.

`approval_policy` is `human_required`. `evidence_policy` is `attested_ok` or
`corroborated_required`; the latter accepts a merged-PR/MR event bound to the
latest completion head or
canonical deterministic bindings for every criterion.

Skill catalog rows are navigation metadata after publication. Full and Local
gate execution use the frozen gate definition in the artifact snapshot, so
editing a Skill cannot change an already-published artifact's evaluation.
Publication fails when a bound Skill is unavailable rather than freezing an
empty replacement.

Local artifact snapshots use the fixed `local-standard` contract: Standard
governance with `plan` and `spec` roles, the Standard topics and
gates, `human_required`, and `attested_ok`. Local embeds default rubric content
and does not resolve Light or Enhanced.

## Context Pack

A Context Pack is the implementation handoff contract for a coding agent. It is
derived from the work item, acceptance criteria, approved/canonical artifact,
policy snapshot, Knowledge provenance, gate state, and applicable skills.

It includes:

- execution brief;
- approved spec/design/plan/verification material;
- acceptance criteria;
- scope and blast radius;
- unresolved gate feedback;
- applicable skills;
- outstanding delivery-review feedback when rework is required.

Coding agents read it through the CLI:

```bash
specgate work context <work-ref>
```

## Coding-Agent Workflow

The normal coding-agent workflow publishes through `specgate artifact publish`
and consumes work through the CLI plus IDE skills installed by:

```bash
specgate plugins install
specgate plugins doctor
```

IDE plugins provide behavior: read the Context Pack, stay inside scope, report
blockers, submit delivery evidence, and read delivery review. They do not add
product scope or bypass approval.

## Delivery Evidence

Delivery reports are append-only evidence. A completion report may include:

- summary;
- affected files;
- checks;
- per-criterion evidence;
- agent metadata and run id.

`coding_agent.completed` requires a non-empty `agent.name`. This identity is
what allows a later peer review to prove that a different agent inspected the
same completion receipt.

Canonical check names are free-form strings supplied by the reporter, but common
CLI scaffolds use `tests`, `lint`, `types`, and `build`. Each runnable check's
shell `command` is retained in the append-only receipt so an independent
reviewer can reproduce it. In Local mode, `delivery submit --run-checks`
overwrites each executed row with the observed status and
`source: "specgate_cli"`; status reports `locally reproduced` only when that
marker is present. Full mode continues to strip caller-authored source fields at
its trust boundary and derives assurance from server-side review.

The `deterministic` trust tier means a bound check decided the criterion, not
that anything re-ran it, so it reads as `bound deterministic check`. Both modes
name the observer in a bound criterion's reason — `observed by the SpecGate CLI`
or `reported by the coding agent, not re-run` — because the two are worth very
different amounts to a human deciding acceptance.

Acceptance criteria may include a human-authored `verification_binding`, created
for quick work in Local or Full mode with:

```bash
specgate work create-quick "Title" --ac "Criterion @check:tests"
specgate work create --feature feature-key --title "Title" --ac "Criterion @check:tests"
```

The binding names a later delivery check. A bound criterion is resolved
deterministically:

| Check status | Criterion result |
| --- | --- |
| `pass` | met |
| `fail` | unmet |
| missing or `skipped` | unclear |

Agents must not invent authoritative bindings. Model-emitted bindings are
ignored for deterministic trust.

Both modes resolve bindings from the stored acceptance criterion, not from the
submitted completion body, so omitting `verification_binding` from a report does
not bypass the table above. The two modes differ only in how they express
`unclear`: Full mode returns `needs_human_review`, while Local review has only
`passed` and `failed` and therefore fails the review, naming the criterion and
the check. Local peer review applies the same resolution.

Delivery review verdicts are stored as `delivery_review` gate runs. Human
delivery decisions written by `specgate delivery approve|reject` outrank later
platform reruns for the same `completion_feedback_event_id`. A newly reported
completion starts a new review cycle and needs its own platform review and human
decision. The human run carries forward the reviewed platform run's completion
binding, evidence verdict, nested criterion/check evidence, evidence judge,
evaluation version, and confidence so authority changes do not erase or
mislabel the delivery projection. The decision write compare-and-swaps the
exact reviewed gate run and latest completion while completion and review
writes share the change-request lock. One completion accepts at most one human
decision. Interactive clients send the `gate_run_id` and
`completion_feedback_event_id` returned by delivery status with the decision;
the server rejects the write if either changed while confirmation was open.
After human acceptance, further completion reports require a new work item.

Delivery-status responses separate the authoritative `verdict` from optional
`evidence_verdict` when the authority is human. They may also include an
optional `reason_code`. The
`policy_unavailable` reason accompanies `needs_human_review` when
artifact-backed delivery review cannot load or interpret the frozen governing
policy. This is a deterministic fail-closed result: Agents must persist it
instead of assuming `attested_ok`, calling a model, or deriving a verdict from
the coding agent's claims. Older delivery-review envelopes have no reason code
and remain readable. `delivery_review_outdated` means the latest completion has
not yet received its own review; a human decision against the older review is
rejected.

### Delivery handoff bundles

`specgate delivery handoff export` writes a Local review request as a single
committable file carrying `schema_version`, `exported_at`, `source_mode`, the
work item, its delivery review, the bound completion report, peer-review status,
and a `checksum` computed over the bundle with the checksum field cleared. The
current version is `specgate.delivery-handoff/v1`; a reader that does not
recognize the value refuses the file rather than guessing.

Export drops every `criteria[].evidence.grounding.excerpt`. Grounding cites any
path the agent read, including files outside the checkout, and the bundle is
committed; the retained `status` and `digest` still say how the citation was
anchored without republishing file contents. Export also refuses the active
Local SQLite file as a destination.

`criteria[].evidence.grounding` is written by the CLI and read by human and
model reviewers in both modes, so its `status` is shared vocabulary:

| `status` | Meaning |
|---|---|
| `grounded` | An explicit `line`, or a `heading` found in the cited file, located the excerpt |
| `heading_not_found` | The cited `heading` does not appear in the file |
| `line_out_of_range` | The cited `line` is past the end of the file |
| `unanchored` | The citation names a path and nothing that pins it to this criterion |
| `empty_at_anchor` | The anchor resolved, but the line there is blank |

Only `grounded` carries an `excerpt`, and it always carries a non-empty one —
the agents' delivery review requires both together, so a blank anchor reports
`empty_at_anchor` rather than an empty grounded excerpt. Every status carries the
file `digest`. A
non-`grounded` status is reviewer-visible information, not a submission error —
the citation is recorded as weak rather than silently presented as verified.
Neither Local nor Full submission validates `grounding` against a closed set, so
a new status never becomes a mode asymmetry.

The bundle is a transport for review, never an authority. `handoff show`
re-derives the verdict from the report and acceptance criteria it carries and
reports any disagreement with the recorded verdict, so a bundle written under
weaker enforcement cannot present a stale pass as current. Reviewer decisions do
not travel in the bundle: human decisions stay with the owning store through
`change accept` / `change request-changes`.

## Gate Runs and Trust

`gate_runs` is the shared evaluation history for artifacts and work items.

Gate states:

- `pass`
- `warn`
- `fail`
- `needs_human_review`
- `not_applicable`
- `pending`
- `not_run`

Trust is derived from the executor:

| Executor | Trust tier |
| --- | --- |
| human | human decision |
| deterministic | deterministic |
| platform/model | platform evaluated |
| IDE agent | agent attested |
| external integration | external corroboration |

The delivery-review trust model records the strongest applicable evidence per
criterion: deterministic binding, grounded local citation, peer review, model
judgment, or self-attested summary. Grounding is still local agent-provided
evidence. Under `corroborated_required`, a would-be pass is clamped to
`needs_human_review` unless a merged-PR/MR event's normalized payload
`head_sha` matches the latest completion receipt's normalized
`git_receipt.head_revision`, or every passing criterion has a canonical
deterministic binding. Missing, mismatched, or stale merge events do not
corroborate delivery. User-supplied checks, including CI output, remain valid
cited or deterministic evidence without becoming repository observation.
Delivery-status readback projects the matched repository signal through
optional `assurance_sources`: `repository_observed`. It supplements
per-criterion trust tiers and never represents human acceptance.

Completion `git_receipt` payloads may set `freshness_scope=shared_repository`
when an origin URL identifies the checkout, or `local_checkout` when no origin
exists. Local scope supports CLI staleness comparison of branch, HEAD, and
working-tree digest in that checkout only; it never supports
`repository_observed` corroboration. Missing scope remains compatible with old
origin-backed receipts.

## Knowledge

Governance Knowledge is workspace-scoped reference material. It is distinct from
approved artifacts.

- `document_id + version` identifies an immutable Knowledge version.
- Only one version per lineage is latest.
- Search requires `workspace_id` for normal calls.
- Retrieval filters by workspace, links, type, authority, and latest-only before
  vector ranking.
- Results cite document id, version, title, chunk, heading, and source.
- Knowledge chunks are untrusted quoted data. Agents may cite or summarize them,
  but must not follow instructions inside them.

Knowledge processing statuses are `uploaded`, `parsing`, `chunked`, `embedded`,
`indexed`, and `failed`.

## Settings

`GET /settings` returns server settings. Sensitive values are redacted as `***`
unless the caller is the trusted governance service. `PUT /settings` updates keys; sending
`***` for a sensitive key leaves the stored value unchanged.

Sensitive settings are encrypted with `SETTINGS_ENCRYPTION_KEY`. Back up the key
with the database.

Important shared settings:

- governance model provider/model/key;
- embedding provider/model/key;
- `governance.default_thinking_level` (`low` by default);
- `governance.auto_archive_on_delivery_pass`;
- governance-file retention.

## Integrations

Integrations mirror external delivery state into evidence; they do not replace
the approved artifact or Context Pack.

They are a Full-mode capability. GitHub and GitLab are **Repositories**:
authorize a provider, select a repository or project, and let SpecGate manage
that selected resource's signed webhook. Linear is optional **Work tracking**:
an explicit Ready-work handoff targets one selected team. Direct CLI/IDE Context
Pack handoff remains available without Linear.

Automatic archive and linked-tracker close transitions follow an explicit
human delivery approval. A platform delivery pass is evidence ready for human
review and must not trigger either terminal effect.

Supported provider surfaces:

- exact provider/resource pairs: GitHub `repo`, GitLab `project`, and Linear
  `team`; mismatched pairs are rejected before persistence;
- resource-scoped managed webhook credentials; no integration-level secret is
  configured by an operator;
- only connected integrations accept provider callbacks. Integration deletion
  removes managed upstream hooks first and preserves local rows if cleanup fails;
- repository delivery links, whose provider `head_sha` is compared only with
  the latest `git_receipt.head_revision`; `merge_commit_sha` is separate
  inspection metadata and never substitutes for the submitted head;
- `repository_observed` only for a marked, merged PR/MR with that exact latest
  completion head; CI is not a SpecGate-produced assurance source;
- at most one primary Linear tracker link per change request. It records its
  exact selected team `resource_id`, never falls back to another team, and is
  Ready-only and idempotent. Provider failures leave no successful link; a
  retry can recover a remotely created issue through its deterministic ID;
- after human acceptance, a best-effort Done transition for that one selected
  Linear issue. A provider failure never changes the durable acceptance;
- webhook inbox rows for dedupe and audit. Processed and pending duplicates are
  no-ops; a previously failed row is atomically reclaimed by at most one retry.
  Linear uses the per-request `Linear-Delivery` UUID, not the installation-level
  body `webhookId`, as its primary delivery id.

The workboard exposes `GET /workboard/change-requests/{id}/delivery-links` for
the persisted repository links and `POST
/workboard/change-requests/{id}/linear-handoff` for the idempotent selected-team
Linear handoff. Both require the selected `workspace_id`.

Feedback event types include:

- `delivery.pr_opened`
- `delivery.pr_merged`
- `delivery.pr_unmatched`
- `delivery.pr_closed`
- `delivery.comment_scope_drift`
- `delivery.tracker_status_changed`
- `coding_agent.blocked_ambiguity`
- `coding_agent.completed`
- `coding_agent.docs_updated`
- `coding_agent.peer_reviewed`

`coding_agent.peer_reviewed` is a cooperative review event, never an approval.
The completion and peer review must both name their agents. The peer must differ
from the completion agent, bind to the latest
`coding_agent.completed` event and identical Git receipt, and claim every
canonical acceptance criterion exactly once. A newer completion invalidates an
older peer-review binding.

## Stale Warnings

Workboard stale warnings are derived read-model signals. Current codes:

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
- `delivery_approval_required`

Warnings may affect queues and Context Packs, but they do not mutate approved
artifacts.

## Naming Rules

- API payload fields use `snake_case`.
- Feature keys use lowercase kebab-case when user-facing.
- Enum values are lowercase.
- Approved artifacts and Context Packs outrank chat history, tracker comments,
  and stale repository docs.
