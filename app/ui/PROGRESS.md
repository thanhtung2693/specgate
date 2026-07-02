# SpecGate UI Progress

## Working Agreement

For page-completion work, use this loop before editing: analyze the current page
and user workflow, design the smallest usable interaction model, apply
`$design-taste-frontend`, `$minimalist-ui`, and `$high-end-visual-design` as
restraint filters, discuss the proposal with a subagent from a user perspective,
review the plan once more for simplicity, missing CTAs, unnecessary UI, and
SpecGate action boundaries, then implement and verify. Work page by page:
Artifacts first, then Reviews, then Work.

Before adding any component, control, card, modal, or action, ask whether it
serves a real user task, real data, direct backend workflow transition, or
allowed governance-agent advisory prompt. If it does not, do not add it. The
same rule applies to actions: do not add a button or menu item unless the owner,
outcome, durability, and SpecGate boundary are clear.

## Current Slice

- Created the `app/ui/` Vite + React + TypeScript app.
- Installed shadcn/ui, Tailwind 4, React Router, assistant-ui, and testing packages.
- Added the base app shell:
  - vertical desktop sidebar;
  - mobile sidebar via shadcn `Sheet`;
  - assistant-ui modal for the governance agent;
  - light/dark mode;
  - workspace and local-user affordance;
  - route surfaces for the main product areas.
- Added an assistant-ui thread/composer surface with a local deterministic adapter.
- Added composer trigger popovers for work items, artifacts, skills, and core governance prompts.
- Reused the landing-page logo as the shell product mark.
- Shifted primary navigation to Work, Reviews, Artifacts, and Settings.
- Added Work as a list-first surface with queue views, reason filters, and an optional lifecycle board.
- Added route-backed work item detail with Context Pack, gates, delivery, and activity in item context.
- Moved Skills into Settings and contextual provenance.
- Applied the minimalist polish pass:
  - warmer monochrome light tokens;
  - flatter bordered surfaces with reduced visual noise;
  - restrained pastel status fills;
  - tighter copy on work and review surfaces.
- Refined Settings for the first layout pass:
  - plugin health is tracked per agent surface;
  - skills are treated as bundled plugin contents;
  - model settings are visible as a dedicated configuration surface.
- Split Settings into modal configuration surfaces:
  - Plugins is the default section;
  - Models separates governance and embedding model controls;
  - Workspace and Integrations have their own adapter-ready panels;
  - a vertical section menu replaces page-level tabs;
  - the right pane is unframed inside the modal.
- Moved Settings from Workflow navigation to the sidebar footer beside the local user/workspace block.
- Moved the governance agent to a floating trigger and made the sidebar toggle persistent in the top bar.
- Added a governance-agent thread-history affordance.
- Added the first Doc Registry workboard data boundary:
  - `VITE_DOC_REGISTRY_URL` switches Work, Reviews, Artifacts, and detail surfaces to `GET /workboard/change-requests`;
  - Work, Reviews, Artifacts, and Features use real registry data or empty/error placeholders, not bundled sample rows;
  - change requests are mapped into shell work items, lanes, attention filters, signals, and Context Pack summaries.
- Hydrated work-item detail tabs from item-scoped workboard endpoints:
  - normalized acceptance criteria;
  - next actions;
  - persisted gate-run history;
  - tracker links.
- Added the governance-agent runtime boundary:
  - `VITE_LANGGRAPH_API_URL` enables `@assistant-ui/react-langgraph` against the `governance` graph;
  - Vite dev can use `/api/agents` with `LANGGRAPH_PROXY_TARGET` to avoid localhost CORS and stale-port conflicts;
  - the deterministic local adapter remains the fallback when the agents service is not configured;
  - the chat UI hides runtime implementation names from users;
  - reasoning-only stream chunks stay behind the single Thinking state, and provider stream errors render as user-readable composer notices instead of blank transcript rows.
- Wired the sidebar account/workspace menu for the layout phase:
  - change local display name;
  - switch active workspace locally;
  - confirm logout and show signed-out state.
- Added Context Pack handoff actions on work-item detail:
  - copy the `specgate://context-pack/...` URI;
  - copy the markdown handoff summary;
  - download the same handoff as `.md`.
  - aligned copied registry Context Pack URIs with CLI canonical change-request ids.
- Built the first Artifact library skeleton:
  - list artifacts from Doc Registry when configured;
  - fall back to representative Context Pack sample artifacts;
  - show selected artifact metadata, documents, and expected gates;
  - align the artifact table with the Work queue table style;
  - preview bundle documents and render embedded Mermaid blocks with source/render and zoom controls.
- Refined Mermaid diagrams in the document preview modal so oversized renders use a
  clipped drag-to-pan viewport instead of visible scrollbars.
- Added contextual governance-agent prompt panels to review and work-detail side
  rails so users can ask scoped questions from the surface they are reviewing.
- Replaced raw gate/status keys in review and detail surfaces with readable
  labels.
- Removed the repeated body-level workspace card; workspace identity stays in
  the sidebar user block and Settings.
- Made the Artifacts page actions concrete:
  - selected artifact opens a detail modal with metadata, documents, expected gates, a conditional Open work action, and Ask agent actions;
  - selected artifact can be restored from `?artifact=<artifact_id>` and updates the query when changed;
  - artifact search and status chips keep large libraries scannable;
  - every document row opens a document preview;
  - long document lists show three files before a show-more control;
  - document rows use compact spacing to keep artifact detail scannable;
  - sample documents have non-empty preview bodies;
  - the artifact library stays full-width; detail is modal-only instead of a persistent side panel;
  - registry timestamps use the shared compact date-time formatter instead of raw ISO strings;
  - dark-mode secondary badges avoid the near-black `rgb(20, 21, 22)` surface;
  - artifact context can be sent straight to the governance agent.
- Refined the Reviews page into a derived triage queue:
  - counts come from current workboard data instead of static cards;
  - filters cover all reviews, needs changes, ready, and gate failed;
  - each row explains the review reason and exposes Open review / Context Pack actions;
  - received governance feedback signals load from `GET /governance/feedback-events?status=received&limit=20`;
  - feedback signal rows stay read-only, with Work/Artifact links but no status triage controls;
  - filtered empty state and sample fallback messaging are visible.
- Made the Work page controls functional:
  - queue search filters work by key, title, owner, status, blocker, Context Pack, and skills;
  - queue views replace competing perspective and attention filter groups;
  - attention reason filters appear only for the Needs attention queue view;
  - a Board/List switch controls whether users see lifecycle lanes or the table;
  - lifecycle lanes and the list table mirror the same filtered queue.
- Refined the Work overview layout after subagent design review:
  - moved queue controls and workspace signals into a compact top control band;
  - flattened the top controls and signal metrics to avoid nested cards;
  - removed the overview work-item preview column in favor of opening the route-backed detail page;
  - moved gate status into the list item cell instead of keeping a separate table column;
  - moved Work updated time to the final list column and widened blocker text;
  - use MacBook-class desktop breakpoints so Reviews, Artifacts, and Work detail do not fall into stacked small-screen views too early;
  - keep route-backed detail pages as the focused inspection surface.
- Applied the Work dashboard feedback triage:
  - renamed the top surface to Governance Board with governance-specific copy;
  - made the compact list queue the default view;
  - made reason filters available for Needs attention and Blocked only;
  - renamed signal metrics to Ready for handoff, Open gate failures, Review SLA, and Blocked by ambiguity;
  - wired signal metrics to their matching queue filters;
  - moved statistics above search and filters, with each statistic as its own card;
  - kept the top Governance Board area unframed instead of wrapping controls in another card;
  - restored kanban work items as cards with compact gate, delivery, blocker, and updated signals;
  - added lane empty states without adding unsupported next-action or evidence fields.
- Removed the UI work-creation path after boundary review:
  - aligned search and Board/List switch heights;
  - main work-item creation stays in the IDE + CLI workspace;
  - the UI no longer shows Create work or creates session-local work items;
  - work items without a Context Pack can send a scoped blocker/readiness prompt to the governance agent.
- Added route classification and confirmation for existing work items:
  - route decisions live in Work detail, not the governance-chat panel;
  - the UI can ask the agents route classifier for a quick/full suggestion and rationale;
  - confirmed route changes persist through the Doc Registry change-request patch endpoint;
  - route controls are only shown before a Context Pack exists because handoff locks the implementation contract.
- Compacted the Artifact list:
  - status moved into the artifact cell;
  - table density now matches the Work list;
  - the list uses Artifact, Version, Impact, Type, and Updated columns now that
    detail moved out of the page body.
- Chose the Artifact detail direction:
  - keep `/artifacts` as the fast library plus selected bundle summary;
  - use the document preview modal as a document inspector for View/Diff;
  - keep the artifact detail as a modal for compact inspection instead of a
    full route;
  - keep version selection and diff inside the document inspector rather than
    duplicating version history in the artifact modal;
  - show real lifecycle evidence that has clear read APIs: artifact-linked
    feedback, feature attachments, and expected gates;
  - defer audit drilling, readiness reruns, conflict/proposal review,
    attachment upload/delete/audience mutation, and feedback triage until those
    workflow owners are explicit.
- Tightened the Artifact detail modal:
  - document rows now render as compact cards in a four-up grid before
    expanding;
  - document cards show relative modified time, using file `updated_at` when
    available and the artifact version timestamp as the current registry
    fallback;
  - Feedback, Attachments, and Expected gates use compact row groups instead of
    nested cards;
  - attachments are read-only here: `gate`/`both` feeds quality gates, while
    `coding_agent`/`both` appears in Context Packs for IDE agents;
  - random files/images shared with an IDE agent do not become attachments
    automatically; attachments require an explicit governed pin, and
    source-of-truth changes must go through artifact proposals;
  - empty lifecycle sections stay quiet and specific rather than adding
    placeholder controls.
- Started the document inspector:
  - renamed the modal from preview to inspector;
  - added same-feature version loading;
  - added a View/Diff toggle with one version selector;
  - View mode has Markdown and Code tabs plus copy;
  - Markdown mode now uses `react-markdown` plus `remark-gfm`, matching the
    assistant-ui markdown stack and supporting lists, tables, inline code, and
    fenced code blocks;
  - Diff mode compares the selected version against the latest version with a lightweight line diff.
- Verified the production build's large chunk warning was Mermaid-specific; Mermaid remains lazy-loaded for document previews and the Vite warning limit now reflects that expected vendor chunk.
- Updated the app shell packaging/details:
  - browser document title is `SpecGate (Experimental)`;
  - favicon uses the shared SpecGate logo asset;
  - contextual actions that send prompts to the governance agent use the robot icon;
  - UI Docker image has an nginx SPA fallback config and module-level docker build/run scripts.
- Tightened governance-agent action boundaries:
  - Context Pack is treated as the IDE/coding-agent handoff contract, not a freeform chat output;
  - chat-backed handoff action is labeled as an advisory blocker/readiness question, not generation;
  - durable workflow transitions remain direct UI/backend/CLI actions.
- Refined the Reviews page after subagent user-perspective review:
  - removed the fixed right summary rail and duplicate queue summary;
  - added review search across work item, owner, route, reason, verdict, and Context Pack identifiers;
  - replaced nested review cards with a compact full-width triage table;
  - renamed row CTAs by intent: review gaps, review handoff, view outcome, and open work item;
  - matched the Work and Artifact table density, moved actions to the last column, and made row actions icon-only with tooltips;
  - kept robot icons only on prompt-sending governance-agent actions.
- Added read-only artifact proposal visibility to Reviews:
  - review rows include row-level Context Pack evidence without duplicate health cards;
  - Context Pack evidence moved into the item cell instead of a standalone
    Evidence column;
  - the primary review row action opens a Review detail modal with verdict,
    reason, owner, evidence, summary, acceptance criteria, activity, and Open
    work item follow-through;
	  - clicking a review row, item key, or title opens the same Review detail
	    modal so the modal is discoverable without knowing the shield icon;
	  - `GET /artifact-edit/proposals` feeds an Artifact proposals queue beside delivery reviews;
	  - each proposal can open a unified-diff inspector through `GET /artifact-edit/sessions/{id}/diff`;
	  - proposal diffs now render file/hunk markers separately from changed lines, and changed Markdown lines render as readable Markdown with add/remove color and wrapping;
	  - approve/save, reject/discard, proposal triage, and conflict resolution remain deferred until verdict ownership is explicit.
- Implemented the governance-agent thread page:
  - the existing View agent threads button now opens a separate in-panel thread page with Back navigation;
  - users can start a new governance thread or switch to a listed thread;
  - runtime implementation names are hidden from the visible UI;
  - LangGraph runtime lists reloadable SpecGate-created governance threads via `client.threads.search()` metadata filters;
  - thread rows show readable titles plus relative activity time instead of local/remote ids;
  - LangGraph title generation calls the agents custom title route and persists the returned title into thread metadata;
  - assistant replies now render as plain transcript text, with a shimmering `Thinking...` state and a Stop action while running;
  - URL thread routes stay deferred; archived-thread browsing and restore are shipped through the thread page.
- Added the first direct Context Pack creation flow:
  - quick-route work without a Context Pack shows a backend-backed Create quick Context Pack action in the Work detail Handoff tab;
  - the action confirms that implementation and tracker handoff stay separate;
  - the UI calls the agents `/workboard/change-requests/{id}/context-pack` route with `quick_mode=true`;
  - Copy URI, Copy handoff, and Download actions become available only when the
    agents response includes the persisted updated work item; artifact-only
    responses show an error and do not create browser-local handoff state.
- Ran a UI scope cleanup after the page/action audit:
  - Work detail now uses Overview, Handoff, Verification, and Activity tabs instead of seven parallel tabs;
  - the Work detail right rail keeps one contextual governance-agent prompt plus the CLI resume command;
  - Needs attention / Blocked reason filters are a compact dropdown instead of another row of chips;
  - route confirmation no longer falls back to session-only state when Doc Registry persistence is unavailable;
  - read-after-write Work overlays from persisted route or Context Pack updates clear on workspace changes so live rows do not bleed across workspace boundaries;
  - Reviews row actions are named as inspection actions and use a neutral eye icon instead of implying approval;
  - the Artifact proposals section stays hidden when there are no proposals in the current view;
  - artifact Open work is shown only when the artifact feature id looks like a routeable work item key.
- Added the first focused tooltip pass:
  - compact/icon-only actions now explain their outcome on hover, including governance-agent chat controls, review row actions, artifact proposal diff inspection, and Mermaid zoom controls;
  - boundary-sensitive actions explain durability: route checking is advisory, route confirmation persists through Doc Registry, governance-agent prompts do not change workflow state, and Context Pack copy/download actions produce handoff material;
  - Settings copy/add/save actions clarify when they need Doc Registry or embeddings without replacing the existing inline warnings;
  - plain filters, tabs, and already-clear navigation stay without tooltips to avoid a noisy help layer.
- Verified the first real-backend UI + CLI + IDE handoff slice:
  - Work detail `Resume in CLI` and downloaded handoff markdown now use the executable `specgate work context <ref>` command instead of the removed `work pick-up` wording;
  - `specgate work context CR-1D0256D8 --json --no-input` successfully resolves a live work item and assembles its Context Pack;
  - the Work detail page shows the corrected CLI command without horizontal overflow at the current desktop viewport;
  - governance-agent chat now renders provider and rate-limit failures as transcript-visible assistant messages rather than leaving a blank response area;
  - `specgate doctor` is clean, while `specgate plugins doctor --agent all` can still warn that the running Codex plugin cache is stale after reinstall until Codex is restarted.
- Verified the real artifact document path:
  - older registry rows can list document metadata while their blob body is missing from the current local storage volume, so the inspector's explicit unavailable state is correct for those rows;
  - a newly published local artifact with inline `documents[]` writes blobs successfully and the UI document inspector renders the Markdown body through the same `/artifacts/{id}/files/_?path=...` API path;
  - the current CLI publish facade is intentionally service-level and `feature_key` based, while the lower Doc Registry `/artifacts` route owns explicit `feature_id`/`version` publishing;
	  - IDE/CLI publish documents should not be base64 encoded; CLI packages can point at local `source_file` or `file_url` entries, and the CLI reads those files into raw UTF-8 content before upload.
	  - the installed local CLI was rebuilt and smoke-tested with `source_file`; the published artifact body reads back as plain Markdown through both Doc Registry and `specgate artifact files --content`.
	- Cleaned up project-local plugin layout:
	  - `plugins/skills`, `plugins/hooks`, `plugins/assets`, and plugin metadata remain the canonical source;
	  - project-local Codex installs now write generated files to `.codex/plugins/specgate` instead of `plugins/specgate`;
	  - stale root `.agents/` and `plugins/specgate/` install artifacts were removed from the repo workspace, and root `.agents/` is ignored as generated IDE state.
	- Cleaned up Settings for the first functional pass:
  - removed dead Refresh, Add, provider-selection, and Save settings controls;
  - Settings opens from the sidebar footer as a modal only; `/settings` redirects back to Work;
  - plugin install commands can be copied directly;
  - the Models tab can load and save the server-side governance and embedding model settings through Doc Registry `/settings`;
  - OpenRouter governance and embedding models load from the public catalog into searchable pickers with seeded/custom slug fallback;
  - saved custom model slugs stay visible as selected rows even when missing from the current catalog response;
  - Models Save lives in the modal footer and only enables after loaded settings change;
  - provider choices use the UI's built-in provider brand icons;
  - embedding and integration sections stay explicit read-only status until a real workflow owner exists;
  - workspace identity inside Settings is read-only; local display-name and workspace switching stay in the sidebar account menu;
  - registry-backed workspace switching no longer shows bundled local workspace choices when the registry workspace list is unavailable;
  - workspace CLI reference commands can be copied from Settings;
  - Knowledge is back as an Experimental Settings section for listing and adding Governance Knowledge through Doc Registry document endpoints;
  - integration rows use provider icons and explain the backend/CLI owner;
  - live integration rows use the provider field for icons, so backend-generated ids do not fall back to the generic plug glyph;
  - Integrations is marked Experimental;
  - when Doc Registry is configured, Settings can add GitHub, GitLab, or Linear integrations through `/integrations` and store a write-only provider API token through `/integrations/{id}/api-token`;
  - hosted GitHub/GitLab and OAuth setup hide Base URL unless the user marks the provider self-hosted;
  - OAuth returns reopen the Integrations section through `?settings=integrations` and then clear the query;
  - OAuth start now requires Doc Registry to return a non-empty provider authorization URL before the browser redirects;
  - connected GitHub/GitLab integrations can list repositories and link a project resource through Doc Registry;
  - connected Linear integrations can list teams and link a team resource through Doc Registry;
  - provider resource candidates without a real `external_key` stay hidden and cannot enable Link, so the browser does not create blank live integration links;
  - webhook reprovisioning, disconnect, secret reveal/rotation, resource delete, and destructive integration delete remain outside the browser workflow.
- Decided not to restore the prior top-level Features page yet:
  - keep the backend Feature model, canonical artifact pointer, and Feature Summary API because they remain current Doc Registry and governance-agent contracts;
  - keep external-LLM summary drafting as a governance-agent/backend capability, with deterministic fallback when models are not configured;
  - avoid re-adding a broad Feature Registry nav surface until the PM/governance workflow clearly needs it;
  - surface feature summary, canonical status, and summary freshness inside existing Work, Artifact, and Knowledge contexts instead of creating another primary page.
- Started that embedded Feature-context path:
  - Work detail hydrates the linked WorkBoard Feature and shows its summary,
    canonical artifact, status, and source version in the Overview tab;
  - Artifact detail resolves the matching WorkBoard Feature by canonical
    artifact or feature id/key and links to Feature detail instead of embedding
    the long summary in the artifact modal;
  - Artifacts → Features is now a compact lookup/detail surface: rows open a
    read-only Feature modal with summary Markdown, source version, canonical
    artifact, related artifact rows, Open work, and Open artifact actions
    without adding a top-level Feature Registry page or regenerate-summary CTA.
- Tightened Knowledge (Experimental) as a provenance surface:
  - rows now show source kind, relative update time, linked feature/request
    chips, and the document summary or notes when available;
  - local search includes provenance fields so users can find the knowledge
    that feeds a feature, request, or Context Pack;
  - detail/version/reindex/delete remain deferred because Knowledge is still
    retrieval provenance, not a general document-management page.
- Verified the governance-agent modal against the live runtime:
  - scoped Work-detail messages render in the transcript;
  - provider/rate-limit errors render as assistant text instead of leaving a
    blank transcript;
  - the Stop response control is present by accessible label while running;
  - the Thinking indicator uses a single ellipsis glyph.
- Fixed the Work-detail CLI bridge:
  - Resume in CLI and downloaded handoff markdown now use the real
    `specgate work context <ref>` command;
  - tests assert the downloaded handoff includes the executable CLI command, so
    UI handoff stays aligned with the CLI surface.
- Fixed MacBook-width layout regressions in the Work and Reviews details:
  - the shell now switches the persistent sidebar to drawer navigation below
    `1024px`, so dense Work, Reviews, and Artifacts tables get the full viewport
    on narrow laptop/tablet widths;
  - the Work detail right rail now constrains long governance context labels and CLI commands instead of overflowing horizontally;
  - the Work detail right rail now stays below the main content until very wide desktop viewports, because viewport breakpoints plus the sidebar squeezed MacBook content;
  - the Review detail modal uses a viewport-bounded scrolling body with a fixed in-modal footer so CTAs stay visible.
- Refined mobile Settings navigation:
  - desktop keeps the two-column Settings modal;
  - mobile now treats Settings as small pages, showing the section list first and a Back button from section content;
  - deep links such as `?settings=integrations` still open the requested section directly;
  - nested Add knowledge and Add integration dialogs keep footer actions visible on mobile while the form body scrolls.
- Added an explicit route state for unknown work item links:
  - `/work/:itemKey` now shows a not-found panel when the key is absent from
    the current workboard instead of silently dropping the user back to the
    board;
  - live-registry detail routes show a loading state while the workboard fetches
    so valid `CR-*` links do not flash as missing;
  - the only action is Back to work, keeping stale-link recovery separate from
    work creation or governance-agent chat.
- Verified artifact document inspection against the live Doc Registry:
  - the CLI-published smoke artifact renders real Markdown content in the
    document inspector;
  - Code view and Copy return the raw Markdown body for IDE/CLI handoff;
  - older artifacts whose blobs are missing from the current local volume keep
    the explicit storage-unavailable state.
- Lowered the floating governance-agent launcher below modal overlays so it no
  longer covers document inspectors, Settings subdialogs, or Review detail
  footers on small screens.
- Tightened dense queue rows after live browser review:
  - Work and Reviews item keys stay on one line instead of wrapping at hyphens;
  - Reviews uses a compact Pack/No pack evidence badge to keep the Reason
    column readable;
  - Review queue rows use relative Updated labels with absolute timestamps
    preserved for detail/hover.
- Aligned the live Work board with the CLI workspace default:
  - Doc Registry `GET /workboard/change-requests` now accepts `workspace_id` as
    a selection filter;
  - the UI discovers the registry workspace through `/api/v1/workspaces` before
    loading Work, so the browser board matches `specgate work list` instead of
    silently showing the all-workspaces view.
- Corrected Settings plugin rows after reinstall smoke:
  - plugin health is labeled CLI-managed because the browser cannot inspect
    local IDE plugin files;
  - copyable plugin commands now use `specgate plugins install --agent ...`,
    matching the refreshed CLI/plugin workflow.
- Cleaned up stale run/review trigger wording from the Doc Registry MCP
  contract:
  - `run_llm_gates` and `trigger_delivery_review` remain durable CLI/MCP/agents
    transitions;
  - the UI continues to surface persisted gate/review state and advisory prompts
    until direct run/review controls have explicit confirmation and ownership.
- Added read-only integration resource visibility in Settings:
  - live integrations now show linked repository/project/team resources from
    Doc Registry alongside default refs and webhook provisioning metadata;
  - malformed or missing `config_json` falls back to neutral webhook labels;
  - resource selection, reprovisioning, disconnect, and destructive delete stay
    out of the browser until those backend-owned flows are explicit.
- Added read-only integration webhook inbox visibility:
  - live integrations show the latest three recorded webhook deliveries from
    Doc Registry with event type, status, correlation id, timestamp, and error
    text when present;
  - the UI does not record, replay, reprovision, or delete webhooks;
  - sample Linear copy now points to the real Doc Registry integration setup
    instead of stale future-connector wording.
- Added read-only Work-detail evidence readback in the Verification tab:
  - live work items load `GET /api/v1/work-items/{id}/evidence` and
    `GET /api/v1/work-items/{id}/evidence/gates`;
  - the UI shows manifests, trust, actor/producer role, digest, submission
    time, evidence counts, and deterministic evidence gate summaries;
  - evidence submission and credential/admin-key flows remain CLI/IDE-agent
    owned.
- Added read-only Work-detail freshness signals in the Overview tab:
  - live work items load CR-scoped warnings from
    `GET /workboard/stale-warnings?change_request_id=...`;
  - the UI shows stale-handoff, tracker contradiction, and external delivery
    signal messages with related work/feature/artifact identifiers;
  - Context Pack regeneration, gate reruns, tracker reconciliation, artifact
    promotion, and evidence submission stay out of browser warning rows.
- Removed MCP/tool catalog visibility from Settings -> Plugins:
  - browser Settings no longer calls `GET /mcp/info` or renders MCP tools and
    resources;
  - tool/resource catalogs remain CLI/IDE-agent execution context, not user
    configuration.
- Added governance-agent thread management:
  - the thread page now supports local title search, inline rename, archive,
    and delete with confirmation;
  - archived governance-chat threads can be browsed through the same search and
    restored to active history through assistant-ui's unarchive adapter;
  - LangGraph rename persists to thread metadata, archive persists as
    `metadata.archived=true`, restore persists `metadata.archived=false`, and
    delete calls the LangGraph thread delete API;
  - cross-workspace history and URL-addressable thread routes remain deferred.
- Added read-only governance policy/profile catalog visibility to Settings:
  - live catalog data loads from `GET /governance-profiles` and
    `GET /api/v1/policies/levels`;
  - the UI shows profile scopes, checks, policy levels, LLM-gate settings, and
    delivery-review requirements;
  - profile activation, policy edits/import/export, dry-runs, and exception
    workflows stay out of the browser until a governance-admin owner is clear.
- Added read-only artifact readiness history to Artifact detail:
  - live artifact modals load persisted rows from
    `GET /artifacts/{id}/readiness-runs?limit=20`;
  - readiness rows show gate, verdict, hint, and timestamp beside documents,
    feedback, attachments, and expected gates;
  - readiness refresh/rerun controls stay out of the browser because those are
    CLI/IDE-agent/backend execution paths.
- Added read-only artifact revision and audit history:
  - live artifact modals load saved draft revisions from
    `GET /artifacts/{id}/revisions`;
  - artifact-scoped audit events load from
    `GET /events?artifact_id=...&limit=20`;
  - revision apply/save, event mutation, and conflict/proposal verdict controls
    stay out of the browser until ownership is explicit.
- Added retrieval-only Knowledge semantic search:
  - Settings -> Knowledge searches indexed chunks through
    `POST /governance/context/search` when embeddings are configured;
  - results show title, authority, score, chunk text, source URI, document id,
    and chunk index;
  - reindex, delete, version creation, and provenance mutation remain outside
    the browser workflow.
- Added narrow integration resource linking:
  - GitHub/GitLab connected integrations list repositories through
    `GET /integrations/{id}/repos?limit=50` and link a project resource
    through `POST /integrations/{id}/resources`;
  - Linear connected integrations list teams through
    `GET /integrations/{id}/linear/teams` and link a team resource through the
    same resource endpoint;
  - linked rows reflect backend-owned webhook provisioning status, while
    reprovision, disconnect, secret reveal/rotation, resource delete, and
    destructive integration delete remain outside browser Settings.
- Re-reviewed artifact conflict surfacing with a subagent:
  - keep `blocked_conflict` visible through existing artifact status chips,
    audit events, and Work-detail `no_conflicts` gate readback;
  - do not add browser calls to `GET /conflicts?services=...` yet because that
    endpoint is a governance pre-publish check keyed by service list, not a
    persisted artifact resolution record;
  - conflict refresh, resolution, override, supersede, prioritization, and
    proposal verdict actions remain CLI/backend/governance-owner workflows.
- Added object-scoped governance policy explanations:
  - Work detail Verification loads `GET /api/v1/work-items/{id}/policy` to
    explain the active governance level, reasons, obligations, and policy
    lineage beside gate/evidence readback;
  - Artifact detail loads `GET /api/v1/artifacts/{id}/policy` to show the
    persisted policy snapshot for that artifact version;
  - policy activation, import/export/editing, dry-runs, profile switching,
    accepted-exception recording, gate reruns, delivery-review triggers, and
    evidence submission remain outside browser UI.
- Added latest delivery-review readback to Work detail:
  - Verification loads `GET /api/v1/work-items/{id}/delivery-status?detail=true`
    for the opened work item only;
  - the Delivery review card shows persisted verdict, hint, review time,
    confidence, outstanding review feedback, per-criterion verdicts, and
    automated checks;
  - trigger delivery review, rerun gates, evidence editing/submission,
    exception acceptance, completion marking, and queue-wide delivery-status
    hydration remain outside browser UI.
- Added read-only governance outcome-health visibility to Settings:
  - Settings -> Governance optionally loads `GET /api/v1/policy-health` and
    `GET /api/v1/outcome-feedback` alongside the existing policy/profile
    catalog;
  - the panel shows aggregate override, rejected-evidence, rollback, and
    escaped-defect counts plus recorded outcome feedback signals;
  - outcome feedback recording, policy activation/import, dry-runs, exception
    acceptance, and policy edits remain outside browser UI.
- Added read-only artifact gate previews:
  - Artifact detail now loads `GET /api/v1/artifacts/{id}/gate-preview` for
    the selected artifact and falls back to the artifact `expected_gates`
    snapshot when the preview is unavailable or empty;
  - expected gate rows can show preview notes, gate version, and executor
    without exposing IDE-agent task payloads;
  - gate-task dispatch/submission, readiness reruns, exception acceptance, and
    proposal verdict actions remain outside browser UI.
- Removed the Settings Diagnostics section:
  - browser Settings no longer calls registry meta/status endpoints just to show
    compatibility information;
  - CLI/IDE-agent diagnostics stay in their owner surfaces.
- Added canonical Context Pack readback in Work detail:
  - live Handoff tabs load `GET /api/v1/work-items/{id}/context-pack` for work
    items with a Context Pack;
  - the assembled registry Markdown is now the preview and the copy/download
    handoff body when available, with the browser-built summary kept as sample
    or unavailable-readback fallback;
  - Context Pack warnings and knowledge provenance render as display-only
    handoff context.
- Added Team rubric Skills management:
  - Settings -> Governance loads `GET /api/v1/skills` when Doc Registry is
    configured;
  - team rubric Skill management remains available when the policy/profile
    catalog fails but `/api/v1/skills` still responds;
  - selecting a registry Skill loads `GET /api/v1/skills/{id}` and shows the
    prompt/rubric body in a scrollable detail dialog;
  - users can add, edit, and delete registry Skills through the existing
    `/skills` create/update/delete routes;
  - editing happens inline in the detail dialog by turning description and
    prompt into text areas rather than opening a second edit modal;
  - Skill rows no longer show a Registry badge, and the Governance home keeps
    users from mistaking rubrics for local plugin health;
  - plugin install/status remains CLI-managed and MCP tool/resource catalogs
    remain absent from the browser.
- Removed legacy governance-thread artifact links:
  - the governance-agent chat panel no longer calls
    `GET /governance/threads/{thread_id}/artifacts` or renders thread-linked
    artifact rows;
  - artifact context now comes from work items, Context Packs, artifact search,
    or explicit artifact ids instead of chat-thread membership.
- Deferred Linear project selection after product-boundary review:
  - the existing Linear resource picker stays team-scoped because outbound
    handoff issue creation currently resolves Linear issues at team scope;
  - `GET /integrations/{id}/linear/projects` remains a backend capability for a
    later contract pass, not a browser control in this safe read-only batch.
- Added narrow General Settings:
  - Settings now includes a General section backed by Doc Registry
    `GET /settings` / `PUT /settings`;
  - the browser can edit only `governance.auto_feature_summary`,
    `governance.auto_archive_on_delivery_pass`,
    `governance.feature_freshness_sla_days`, and
    `governance.artifact_stale_days`;
  - auto-archive is backend-owned and only archives a work item after a
    persisted delivery review passes, using the `specgate-auto-archive` actor;
  - governance-file retention, confidence thresholds, retry/timeout knobs,
    rollback flags, MCP token/config, and speech-to-text settings remain
    read-only/deferred because they need operator or governance-admin ownership;
  - manual archive remains a contextual Work/CLI action with actor/reason/audit.
- Fixed Settings and artifact modal regressions:
  - Settings -> Plugins keeps long Skill/plugin text inside the modal pane
    instead of clipping to the right;
  - Artifact detail uses a fixed viewport-bounded grid body so the modal scrolls
    through large document/evidence bundles.
  - shared modal footers no longer use negative bottom margins, so detail/edit
    actions stay inside the viewport instead of clipping at the bottom.
  - live Doc Registry Work views start on an empty/loading registry placeholder
    instead of flashing bundled sample work during refresh.
  - Reviews and Artifacts now keep bundled sample rows/diffs out of runtime
    loading, empty, and error states; placeholders give next-step copy instead
    of briefly showing fake data.
  - live Reviews feedback signals require real registry feedback ids before
    rendering, matching proposal-row id handling.
  - live artifact feedback rows require real registry feedback ids before
    rendering in the Artifact detail modal.
  - live artifact evidence rows now require real registry row ids for
    attachments, readiness runs, audit events, and saved revisions before
    rendering in the Artifact detail modal.
  - live Work tracker links require real registry identifiers and URLs before
    rendering, so incomplete rows do not become `tracker-*` links or `#`
    actions.
  - live Work evidence manifests require real registry `record_id` and
    `manifest_id` before rendering in Verification.
  - live Work gate-run history requires real registry gate-run ids before
    rendering in Verification.
  - live governance policy lineage requires real policy keys before rendering
    as provenance.
- Audited seeded registry Skills with a subagent and `writing-skills` criteria:
  - keep the six seeded Skills, but rewrite them as bounded gate rubrics rather
    than generic IDE-agent/OpenSpec/SpecKit review helpers;
  - highest priority rewrite is `review-impl`, which is bound to
    `delivery_review` but currently reads like a planning-bundle review;
  - added a Doc Registry seed overwrite mode for intentional starter-rubric
    refreshes while keeping default startup seeding preserve-only.
- Added temporary local login/onboarding:
  - first run without localStorage opens an onboarding form for email, username,
    display name, and workspace;
  - when Doc Registry is configured, onboarding calls
    `POST /api/v1/identity/bootstrap` and stores the returned local
    user/workspace selection;
  - live onboarding no longer pre-fills bundled local workspace names or
    choices when the registry workspace list is unavailable;
  - logout clears only the browser-local UI session and returns to onboarding;
  - workspace switching reads registry workspaces when available and passes the
    selected workspace id into Work board registry requests;
  - stale browser-local `local-*` workspace ids are ignored in live registry
    workboard requests so the adapter can discover a real registry workspace;
  - live registry workspace choices require real registry ids, so incomplete
    workspace rows are ignored instead of becoming local fallback options.
- Continued the live-mode sample fallback audit:
  - live Work detail no longer fills an empty registry acceptance-criteria
    response with sample criteria from the workboard row;
  - live Work detail readback failures for acceptance, gates, tracker links,
    linked feature context, policy, evidence, and delivery review now show
    explicit registry errors instead of hiding sections or implying no records
    exist;
  - live Context Pack readback failures now keep Copy URI available but disable
    Copy handoff and Download `.md`, so IDE agents are not handed browser-built
    fallback markdown when the canonical registry pack is unavailable,
    unassembled, or missing Markdown;
  - empty acceptance sections now show a CLI/IDE workflow placeholder instead
    of a silent blank list;
  - UI docs now state that missing or unreachable Doc Registry mode shows
    empty/error placeholders rather than bundled sample data.
- Tightened live artifact document inspection:
  - live artifact document-list failures now show explicit Doc Registry errors
    instead of empty-document copy or sample document cards.
  - live artifact version-history failures now keep Diff disabled and explain
    that no fallback version comparison is shown.
  - when Doc Registry has document metadata but the local content body is
    unavailable, the inspector disables Copy and Diff instead of copying an
    empty placeholder or comparing fake content.
  - live artifact gate-preview failures now show an explicit Doc Registry error
    and do not render fallback expected gates from artifact list metadata.
  - live artifact evidence-section failures now show explicit Doc Registry
    errors instead of empty-record copy for attachments, feedback, readiness
    history, saved revisions, and audit events.
  - linked feature and artifact policy readback failures now stay visible as
    live-mode errors instead of hiding feature context or implying no policy
    explanation exists.
  - live artifact rows now require a real registry artifact id, and live
    document rows require a real file path before the browser renders them or
    issues follow-up artifact/file requests.
  - live linked Feature rows and expected gate preview rows require real
    registry feature ids/keys and gate keys before rendering.
  - live Knowledge documents and semantic search hits now require real
    `document_id` values before Settings renders them.
  - live Governance outcome feedback rows now require real registry ids and
    work-item references before Settings renders them.
  - live team rubric Skill rows now require real registry ids before
    inspect/edit/delete actions appear.
  - live integration rows now require real registry ids before Settings renders
    them or fetches linked resources/webhook events.
  - integration resource pickers now require real provider keys from Doc
    Registry before showing selectable repository/team rows or enabling Link.
  - live Governance profile and policy-level rows now require real registry
    keys before Settings renders them.
  - static integration status rows, governance-agent mention catalogs, stale
    sample source tags, and unused legacy public SVG assets are removed from the
    runtime source; Work, Reviews, Artifacts, Features, and Settings now show
    real registry data or explicit placeholders only.
- Fixed Settings deep-link behavior while the modal is already open:
  - route/query changes such as `?settings=governance` now open the requested
    mobile section page instead of leaving the modal on the section list;
  - direct Settings entries still keep the Back-to-sections mobile affordance,
    preserving OAuth-return and query-driven settings flows.

## Next Candidates

1. Keep URL-addressable governance-agent thread routes deferred until thread
   ownership and artifact-link source of truth are clearer.
2. Review whether any remaining Settings or Work-detail read APIs provide
   inspection value without adding browser-owned workflow transitions.

## External Review Triage

Another design review of the Work dashboard highlighted useful governance
framing, but it did not have full product/data context. Apply these notes
selectively:

- The Work page now defaults to the compact list queue; the lifecycle board is
  a secondary visualization behind the Board/List switch.
- Low-risk Work improvements applied in this pass:
  - rename the old "Queue view" area to a governance-specific surface label;
  - clarify the queue subtitle around approved specs, handoffs, evidence,
    verification failures, and reconciliation;
  - show reason filters for Needs attention and Blocked, not for every queue;
  - make signal metrics clickable where they map cleanly to an existing queue
    filter;
  - add empty lane messages;
  - make board cards carry the same gate, delivery, blocker, and updated signals
    shown in the list.
- Defer until the Doc Registry/workboard contract exposes richer fields:
  - canonical `governance_state`, `attention_reason`, `next_action`,
    `evidence_state`, `verdict_state`, `blocker_reason`, and `updated_at`;
  - richer list columns like Governance state, Next action, and
    Evidence/Verdict;
  - lifecycle-specific chips such as Context stale, Reconciliation required, or
    Tracker sync failed when those states are not yet present in API data.
