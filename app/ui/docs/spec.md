# SpecGate UI Specification

## Purpose and boundary

SpecGate UI is the responsive inspection and human-decision surface for
governed delivery. It shows work, artifact review, and configuration backed by
Doc Registry. IDE agents and the CLI remain the primary places to author
specifications, create work, implement code, run checks, and submit delivery
evidence.

The UI never invents live identifiers, rows, document bodies, or governance
results. With no Doc Registry connection, an empty response, or a failed
request, it shows a setup, empty, or error state instead of representative
sample data.

## Routes

| Path | Surface | Behavior |
| --- | --- | --- |
| `/` | Entry | Redirects to `/work`. |
| `/work` | Work queue | Lists governed work and status signals. |
| `/work/:itemKey` | Work detail | Shows the selected item or an explicit not-found state. |
| `/reviews` | Review queue | Presents artifact decisions and delivery evidence. |
| `/artifacts` | Artifact library | Inspects versioned artifacts and their evidence. |
| `/knowledge` | Knowledge library | Manages active-workspace Governance Knowledge versions. |

Unknown routes redirect to `/work`. Settings is a modal, not a route. A
`?settings=<section>` query opens a valid Settings section and is then removed
from the URL. OAuth returns may use `?settings=integrations`.

## Shell and attribution

The shell has Work, Reviews, Artifacts, and Knowledge navigation; a theme toggle; an
identity/workspace menu; Settings; and, when available, a governance-agent launcher. Desktop
uses a persistent sidebar. Narrow viewports use the sidebar sheet so tables
retain their usable width. The browser and sidebar title are `SpecGate
(Experimental)`. A keyboard-visible skip control moves focus past the persistent
navigation to the page content, and animated agent thinking indicators respect
the browser's reduced-motion preference.

Before entering the shell, users provide display name, username, optional email,
and workspace name. This is attribution, not authentication or authorization.
The UI requires Doc Registry and must bootstrap the selection there. Without a
configured registry, it points users to Full-mode setup instead of fabricating a
browser-local workspace. A bootstrap failure keeps the setup form open with an
actionable diagnostic. Returning sessions reuse their saved selection. The
sidebar can switch among available workspaces; it does not expose a client-only
identity editor. It has no logout, invite, role, or member-removal flow.

## Work

The Work overview is read-only. It provides search, lifecycle and reason
filters, list-first and board-secondary views, workspace signals, and explicit
refresh.
`Ready` means approved source is available for IDE/CLI pickup; it is neither an
implementation nor a review queue state. Delivered items appear separately from
attention work.

Work detail has Overview, Handoff, Verification, and Activity tabs. It presents
delivery trust before gate detail, acceptance criteria, gate evidence, policy,
stale warnings, tracker links, and Context Pack state. Delivery trust keeps four
facts separate: the evidence assessment, its assurance source, the human
acceptance decision, and the recorded Git receipt. It also exposes the reviewer,
peer-review freshness, per-criterion trust tier, and verification binding.
Server-observed `repository_observed` assurance comes from delivery status only
when a merged PR/MR event matches the latest completion head, so it remains
visible even when no individual criterion carries that trust tier. CI is not a
first-release assurance source.
The displayed provider `merge_commit_sha` is inspection metadata; it never
replaces the submitted PR/MR `head_sha` in that comparison.
Evidence readiness alone never renders as human acceptance. Model-reviewed
evidence carries a persistent reminder that the model did not inspect the code,
replace CI, or make the human decision.
Deterministic-check evidence is labelled as deterministic and never receives
that model-review disclaimer.
For a current platform review, Work detail exposes **Accept** and **Request
changes** regardless of its advisory evidence verdict. A deliberate
confirmation identifies the exact work item and exact reviewed completion and
attributes the decision to the selected username. Request changes requires
actionable feedback. The request is scoped to the selected workspace, and Doc
Registry remains authoritative for stale, self-review, duplicate,
missing-review, and workspace errors. Success refreshes work detail, delivery
status, and the review queue.
Work-list delivery labels follow the same boundary: a platform pass is `Ready
for human review`, while only the server-derived human-approved Delivered phase
is `Accepted`.
Every Work-detail registry read carries the active workspace; without one, the
UI makes no product-data request.
Context Packs can be copied or downloaded only when their registry-derived
readback is assembled. Artifact-backed work includes approved artifact context;
quick work derives its handoff from persisted intent and acceptance criteria.
CLI resume/context commands are copyable; the UI does not create work or
pretend that an advisory agent prompt changed governance state.

For a Ready work item with no persisted tracker link, Handoff keeps the direct
Context Pack copy/download path and may offer an optional Linear handoff. Its
team selector loads connected Linear team resources only when opened. A
successful handoff re-reads the persisted tracker link and then replaces the
create control with that linked issue. The UI does not create a second issue.

## Artifacts and reviews

Artifacts is a read-only library. It supports search, status filtering, version
lineage, document inspection, Markdown/code rendering, copy, and version diff.
Superseded versions stay hidden until explicitly included. Artifact detail may
show linked feature context, attachments, feedback, readiness history, audit
events, expected gates, and policy snapshots when those
registry records exist. Missing document content, version history, policy, or
evidence is explicit and disables dependent actions. Search and filters remain
visible while data loads, then disappear for a settled empty library or review
queue. Registry failures render an unavailable state, never an empty-workspace
claim.

Reviews owns durable artifact decisions. It lists draft and needs-changes
artifacts and delivery evidence. Artifact approve/request-changes actions use
the backed Doc Registry flow. Delivery rows link to Work verification, where
ready evidence can receive the human delivery decision. The UI does not expose
a separate Features browser, artifact-attached agent action, or
feedback-triage workflow.

## Settings

Settings sections are General, Models, Workspace, Governance, and Integrations.
On mobile, the section list and content act as consecutive pages. On desktop,
they share a two-column modal. Only General and Models have modal-footer Save
actions.

- **General** reads and writes auto-archive-after-human-acceptance and the
  artifact-retention-sweeper toggle. Its destructive workspace cleanup action
  requires inline confirmation plus an active workspace, which it sends to the
  maintenance endpoint.
- **Models** configures governance and embedding providers, models, thinking
  level, and masked provider keys through Doc Registry settings. OpenRouter
  model lookup is optional; unavailable lookup falls back to built-in choices
  and custom model IDs remain usable.
- **Workspace** shows current workspace identity and read-only members.
- **Governance** is action-first: team rubric Skills appear first with explicit
  Add and Manage actions plus in-place Retry after list failures. Skill edits
  affect later resolution and lookup; existing snapshots retain their recorded
  values. A collapsed native Policy reference loads automatic policy tiers only
  when opened and expands compact rows to show gates, evidence, roles, topics,
  and raw identifiers. It is not an IDE-plugin manager.
- **Integrations** groups GitHub and GitLab under **Repositories** and Linear
  under **Work tracking**. It adds connections, starts hosted OAuth by default,
  retains API-token setup for self-hosted or advanced use, links provider
  resources, and shows read-only resources and recent webhook delivery state.
  Repository provider copy is limited to marked PR/MR and exact submitted-commit
  signals; Linear describes optional approved-work handoff. It is not a full
  integration-admin surface.

Knowledge configuration, plugin installation,
webhook-secret rotation, destructive integration management, policy
activation/default selection, policy-tier editing, exceptions, gate execution,
feedback and governance-health administration,
Skill history/rollback, and workspace-wide effective-governance summaries
remain outside browser Settings. Effective policy is resolved and snapshotted
per artifact or work item. Plugin installation and health belong to the CLI and
IDE.

## Knowledge workspace

`/knowledge` lists the active workspace's latest Governance Knowledge versions
and supports local search/type/status filters, file upload, preview and history
inspection, failed-ingestion retry, and exact-version deletion. Uploads and
link curation create immutable versions; existing versions are never edited or
overwritten.

The shell owns the route's single page-level `Knowledge` heading; the workspace
surface begins with the sequential `Knowledge library` section heading. When a
persisted attribution profile is reopened, the shell first lists the
appliance's workspaces. If the saved workspace id is absent, it clears the stale
browser selection and requires explicit setup; merely opening a new appliance
must not create users or workspaces. A workspace-list failure leaves the saved
shell visible but does not enable Knowledge without a validated workspace.

Upload requires a configured local embedding provider and model. When Doc
Registry reports `embeddings_enabled: false`, upload is disabled and the UI
links to Settings → Models. Local mode does not require S3.

The browser is an inspection, upload, and curation surface only. Repository
tools and user-selected source frameworks remain the authors; browser Knowledge
text authoring is excluded.

## Governance agent

The governance agent is an optional advisory assistant-ui modal. It appears
only when `VITE_LANGGRAPH_API_URL` is configured and a fail-closed health check
reports chat configured and reachable. Otherwise the launcher is hidden; no
fake runtime or setup placeholder is shown. Full-mode core work, review,
artifact, Knowledge, and settings surfaces remain usable without chat.

The UI does not expose the appliance's ephemeral thread history or
thread-management controls. Switching workspaces remounts the runtime and
creates a workspace-tagged LangGraph thread for new runs without a separate
title or sidebar-index service. The composer has no `@` entity
insertion and offers only **Artifact summary**, **Readiness results**, and
**Knowledge search** prompts, matching the graph's four read-only tools. An
empty workspace selection creates no product-data request, and
workspace-owned data adapters reject an unscoped call. Enter sends;
Shift+Enter inserts a newline.

## Data ownership

Doc Registry owns persisted work, artifacts, settings, governance catalog,
skills, integrations, and identity/workspace records. The browser owns only
presentation state and the active UI selection, including the cached
attribution selection used to reopen a registered workspace.

Integration catalog, resource, webhook-event, token, and OAuth requests include
the selected workspace. Switching workspace clears the integration list,
expanded details, and pending link dialog; no request is made without a
selected workspace.

Real registry rows must carry their real identifiers before the UI renders
follow-up links or requests. The UI ignores incomplete rows rather than
constructing IDs from names, indexes, or local state.

Verification reads persisted repository delivery links alongside delivery status.
It displays the provider URL and compares a merged link head only with the
latest recorded completion receipt: opened links require merge, an exact merged
head is observed, a mismatched merged head is stale, a merged link without a
receipt asks for one, and a closed link asks for replacement. This is display
state only; the server remains the delivery-verdict authority.

## Verification

Run from `app/ui`:

```bash
npm run lint
npm run test
npm run build
```

For visual changes, also inspect the running app at desktop width and one
narrow viewport.
