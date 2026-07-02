# Spec — Agent Module

## 1. Scope

The agent module is a **headless governance-ops service** SpecGate calls server-side. It does not draft PRDs, specs, or implementation plans — creation happens in the developer's IDE. It runs readiness gates, post-build delivery review, route classification, and context-pack compilation, and it exposes a thin domain-specific chat surface over governed artifacts.

It is built on LangGraph + LangChain's `deepagents` library. The runtime is **one thin governance-chat node + a deterministic governance-ops HTTP layer** (`webapp.py`). Governance services are plain async functions invoked by FastAPI handlers; they do not route through the graph.

Layered docs:

- `docs/prd.md` — module intent
- This spec (`docs/spec.md`) — module contract
- `docs/governance/docs/spec.md` — governance sub-graph contract

## 2. Inputs

- User chat text over the LangGraph `messages` channel (any language)
- Governed artifact ids, `feature_id`, `change_request_id`
- The artifact's snapshotted governance profile (`gates_profile_snapshot_json`) driving readiness — including its `enabled_gates`, `required_topics`/`roles`, and the optional `gate_skills` map (`{gate_key → skill_name}`) bound to rubric Skills
- Reusable **Skills** (team rubrics) fetched from doc-registry (`GET /skills`): when a profile's `gate_skills` binds a Skill to a gate, the readiness and delivery-review judges inject that Skill's prompt as a "Team Policy" rubric on top of the gate's built-in prompt (gate-consumes-Skills; unbound or missing → built-in prompt only)
- Registry event updates + tracker delivery feedback (evidence intake via doc-registry webhooks)

## 3. Outputs

- Readiness / quality-gate verdicts (artifact- and change-request-scoped)
- Post-build delivery review verdicts
- Route classification (proportional ceremony)
- Compiled context packs (IDE handoff)
- Thin chat replies on the LangGraph `messages` channel
- Run status and trace events

## 4. Runtime topology

```
   LangGraph `governance` graph                FastAPI `http.app` (webapp.py)
 ┌───────────────────────────┐        ┌──────────────────────────────────────┐
 │  governance_chat (1 node) │        │  Governance-ops HTTP endpoints:        │
 │  build_supervisor(        │        │   run-llm-gates / run-readiness        │
 │    model=governance_ops,  │        │   review-delivery                      │
 │    tools=[the 4 governance│        │   classify-route / context-pack        │
 │     tools; see §5 below]  │        │   quick-work-item                      │
 │  )  — no drafting, no HITL │        │   thread title                         │
 └───────────────────────────┘        └──────────────────────────────────────┘
   ▲ chat surface                        ▲ called by doc-registry / UI
   │ (messages channel)
```

The only LangGraph graph is `governance` → `governance_chat.py:graph`: one node built with the generic `main_agent.build_supervisor` wrapper on the dedicated governance-chat model (`build_governance_ops_model()`), exposing governance-ops as tools and no drafting tools. There are no drafting sub-agents, no per-node overlay system, and no HITL drafting interrupts. Governance operations are plain async functions in `board/` and `quality_gates/` invoked directly by `webapp.py` FastAPI handlers — they do not route through the graph. `webapp.py` reaches thread state via the LangGraph SDK loopback (`langgraph_sdk.get_client`), so the chat surface (thread title) is independent of which graph is served.

## 5. Governance operations

Deterministic Python services invoked by `webapp.py` HTTP handlers (and a curated subset bound as governance-chat tools). Doc Registry is the source of truth; services use the REST API in `doc-registry/docs/spec.md` and do NOT couple to SQLite or S3 directly. The chat node binds four read/run tools (`get_artifact`, `get_artifact_documents`, `list_artifact_readiness`, `run_artifact_readiness`); every other governance operation is an HTTP endpoint.

### Quality-gate judge

`governance/quality_gates/judge.py` runs injected-model LLM gates over artifact bundles and posts verdicts to Doc Registry. `board/quality_gates.py::run_llm_gates_for_change_request` keeps the workboard path (`POST /workboard/change-requests/{id}/run-llm-gates`), while `board/quality_gates.py::run_llm_gates_for_artifact` is the SP1 artifact-scoped readiness path exposed at `POST /artifacts/{id}/run-readiness`. The artifact path reads the published artifact's `gates_profile_snapshot_json` and drives readiness by profile: `enabled_gates` (which gates run), `required_topics` (narrows the completeness roll-up AND tags those topics `[REQUIRED]` in the prompt so the model judges required-vs-optional per change-type), and `required_roles` (a deterministic `required_roles_present` signal — warn when a required document role is absent). Results persist through Doc Registry `POST /artifacts/{id}/readiness-runs/refresh`. `required_evidence` is delivery evidence and is out of scope for readiness.

**Snapshot parsing.** `quality_gates/profile_snapshot.py::parse_profile_snapshot` is the canonical dual-version snapshot parser. It detects `specgate.policy/v1` vs `legacy/v1` (implicit or explicit) from the `snapshot_schema_version` discriminator and projects both into a `ProfileSnapshot` frozen dataclass. Unknown explicit versions raise `UnsupportedSnapshotVersion` (fail-closed). The parser is used by `board/quality_gates.py`. **`governance_level`** is populated from `specgate.policy/v1` snapshots and passed through in the readiness result dict (`run_llm_gates_for_artifact` returns `{"governance_level": "<level>", ...}` when non-empty). Callers catching `UnsupportedSnapshotVersion` at the HTTP boundary emit a blocking compatibility result (zero evaluations, `compatibility_error` field) so incompatible profiles fail closed rather than silently defaulting. `legacy/v1` snapshots yield `governance_level=None` without error.

**Context pack.** `generate_context_pack` reads `governance_level` from the source artifact's `gates_profile_snapshot_json` (via `parse_profile_snapshot`) and includes it in its return dict when non-empty (`{"governance_level": "<level>", ...}`). Snapshot read failure is best-effort — a missing level does not block pack generation.

**Routed, labeled sections.** `client.aload_artifact_markdown_bundle` loads `prd`, `spec`, `tasks_fe`, `tasks_be`, `tasks_qa`, `rollout`, `risks`. `evaluate_all_gates` assembles a labeled markdown string (`## PRD`, `## Spec`, `## FE plan`, `## BE plan`, `## QA plan`, `## Rollout`, `## Risks`; empty sections omitted) and routes each gate only the sections it needs:

| Gate | Sections |
| --- | --- |
| `rollback_plan_present` | Spec + Rollout + Risks |
| `acceptance_criteria_edge_cases` | PRD + QA plan |
| `acceptance_criteria_verifiable` | PRD + QA plan (flags vague/non-testable criteria → restated suggestion; upstream half of the post-build delivery review) |
| `success_metric_measurable` | PRD + Spec |
| `scope_clear` | PRD + Spec |
| `implementation_plan_traceable` | Spec + FE plan + BE plan + QA plan |

Each gate receives every document its judgment depends on, not just the PRD.

**Feature reference attachments.** `board/quality_gates.py` also fetches the feature's reference attachments (`client.alist_feature_attachments(feature_id)`, Doc Registry `GET /features/{id}/attachments`) and threads the `gate`/`both`-audience rows (rendered by `governance/attachments.py::render_attachments_section`) into the `acceptance_criteria_edge_cases` and `scope_clear` gate inputs — bug repros and examples ground edge-case + scope review. `coding_agent`-only attachments never reach the gate. The audience flag (`gate`/`coding_agent`/`both`, default `gate`) is set when the attachment is created; reaching a consumer is an explicit opt-in, not automatic.

**Work-type-aware applicability.** The CR `work_type` is threaded into each gate prompt. The model picks `not_applicable` only when the gate genuinely does not apply to this change type (e.g. a research spike or pure docs change may need no rollback plan or success metric); a section the change type *should* have but is missing/empty is `fail`, not `not_applicable`. The decision stays in the model — no keyword rules.

**Evidence per verdict.** Each `GateJudgment`/`GateEvaluation` carries an `evidence` string — a short supporting quote or the section name the model relied on — surfaced into the posted `evaluations[]` payload alongside `gate`, `state`, `hint`, `confidence`.

**Confidence escalation.** `resolve_gate_state` escalates a low-confidence `pass` *and* a low-confidence `fail` to `needs_human_review` (a low-confidence fail is as unsafe to auto-trust as a low-confidence pass); `warn` and `not_applicable` pass through verbatim. The floor is `governance.gate_confidence_threshold`.

**Gate verdicts ride the handoff.** Unresolved gate verdicts (latest-per-gate `warn`/`fail`/`needs_human_review`) are rendered into the Context Pack as an **"Unresolved Quality Gates"** section — by `board/context_pack.py::render_context_pack` for the quick/Execute-anyway lane and by the doc-registry on-read renderer for the full lane — so a handoff made past the Execute-anyway escape hatch still delivers the gate hints (e.g. the `acceptance_criteria_verifiable` restatement) to the coding agent rather than silently dropping them.

### Post-build delivery review

The pre-handoff quality gates judge the *plan*; the delivery review judges the *built result* — the Reviewer box's after-the-agent-builds step in the AI-DLC loop (Governance Ops → Builder → Quality Gates → Reviewer → Pass/Fail). `quality_gates/delivery_review.py::review_delivery` takes the work item's acceptance criteria + the coding agent's latest `coding_agent.completed` feedback payload (per-AC `criteria` claims, automated `checks`, `affected_files`, `evidence`, `summary`) and judges each criterion `met | unmet | unclear` via structured output. The overall verdict is enforced deterministically — **pass** only when every criterion is met AND no check failed; any `unmet`/failed check ⇒ **fail**; an `unclear` criterion (or no criteria to judge) ⇒ **needs_human_review** — then the same `resolve_gate_state` low-confidence downgrade applies.

`board/delivery_review.py::review_change_request_delivery` is the runner (mirrors `run_llm_gates_for_change_request`): it loads the CR's `acceptance_criteria_json`, finds the newest `coding_agent.completed` event for the CR via `client.alist_governance_feedback_events`, runs `review_delivery`, and persists the verdict as a `delivery_review` GateRun via `POST /workboard/change-requests/{id}/gate-runs/refresh` (the gate-run store appends eval-only gates, so no schema change). The per-criterion verdicts + checks ride the evaluation's `evidence` string as JSON (`{criteria, checks}`) so the UI can render per-AC results. Exposed at `POST /workboard/change-requests/{change_request_id}/review-delivery`; returns `verdict=null` with `reason="no_completion_report"` when no completion evidence exists. When no platform LLM is configured, or a configured model/provider is temporarily unavailable, the runner derives the review from the coding agent's per-criterion claims and automated checks instead of returning a no-verdict response. The reviewer prompt treats schema-valid file/test evidence as acceptable evidence and does not require pasted code excerpts when the report cites specific tests, changed files, and behavior. On **fail**, the doc-registry on-read Context Pack folds the unmet criteria + failing checks into an "Outstanding Review Feedback" section so the next handoff carries the gaps (the Fail → back-to-Builder edge).

### Readiness check (minimum-executable-contract over flexible documents)

`quality_gates/completeness.py::evaluate_spec_completeness` checks whether a work item is ready to hand to a coding agent by **locating readiness topics across the published documents, resolved by role** — not by fixed filenames. The harness reads the artifact's documents via `client.aload_artifact_bundle_by_role` (`GET /artifacts/{id}/files` → group by role → fetch by path), so it works over **any spec format** (PRD/spec/BE/FE/QA; OpenSpec; Spec Kit; custom). The topic set leads with the **minimum-executable-contract** (goal, scope, non-goals, acceptance criteria, constraints, risks, verification) — the must-have-before-handoff safeguard — followed by build-readiness depth (users/roles, workflows, screens/states, data model, permissions/security, integrations, edge cases, metrics, observability, phased tasks). Each topic is marked `covered | partial | missing | not_applicable`; verdicts come from the model (structured output, no keyword rules); the overall state is enforced deterministically — **advisory: any missing/partial *required* topic ⇒ `warn`, never `fail`** — with the shared low-confidence downgrade. The completeness gate gets the **full** by-role bundle (every role, including `unspecified`/`custom:*`); the routed single-topic gates map onto roles (AC/scope → `spec`; tasks → `plan`; QA → `verification`; rollout/risks → `reference`). It persists as a `spec_completeness` GateRun whose `evidence` carries the per-topic detail as JSON; its summary rides the Context Pack via "Unresolved Quality Gates". Profile-specific required sets (per change-type) arrive with SP1; auto-run-on-publish is a follow-on (today readiness is on-demand via "Run gates").

### Feature reference attachments

`governance/attachments.py` is the shared helper (`filter_by_audience`, `render_attachments_section`) over a feature's reference attachments (Doc Registry `GET /features/{id}/attachments`). Audience-gated consumers: the **quality gate** (above) reads `gate`/`both` rows; the **coding-agent context-pack** (`board/context_pack.py::render_context_pack`) reads `coding_agent`/`both` rows into a `## Reference Attachments` section. The audience flag (default `gate`) throttles these cost/risk-sensitive surfaces — over-sharing bloats the handoff and adds review noise.

File/image rows render as Doc Registry content-proxy URLs (`/governance/files/{id}/content`), never S3 URLs. Audience is an explicit user choice (optionally LLM-suggested over the title/note — never keyword rules over that user text).

IDE-agent uploads are not automatic attachments. A file, screenshot, or log sent
to a coding agent remains implementation-local context unless the user or agent
explicitly pins it as a feature attachment with an audience. If the uploaded
material changes the governed contract, the agent must draft an artifact-update
proposal instead of pinning it as supplemental context.

**Keying.** Attachments are keyed by the feature **key** (the value feature-backed artifacts publish as `feature_id`), not the feature UUID. The board consumers resolve it from the artifact's `feature_id` (gate: `aget_artifact(lead_artifact_id).feature_id`; context-pack: `feature.key`) so the lookup matches what the UI writes — a UUID lookup silently returns zero attachments. Featureless quick-route Context Packs have no feature attachment scope.

### Route classifier — proportional ceremony (FR-6.1)

`governance/board/route_classifier.py` (`classify_route`) selects the right amount
of planning ceremony for a work item: given the change request's `title`,
`intent_md`, `work_type`, and the feature's `impact_level`, it returns a
`RouteDecision` — `route` (`quick` | `full`), `confidence`, and a one-sentence
`rationale`. The decision is structured output (no keyword/rule matching over
content), the configured governance-ops model is injected (the same model family used
by the quality-gate judge), and a
sub-threshold-confidence `quick` is downgraded to the conservative
`full` route via `resolve_route` (reusing `governance.gate_confidence_threshold`).
Any model error also falls back to `full` — a small-change shortcut is never
taken on thin evidence. The classifier only *suggests*; the route decision is
confirmed or overridden by a human on the work item.

`board/route_suggestion.py::classify_route_for_change_request` fetches the
change request + its feature from Doc Registry (impact level is best-effort: an
artifact-level attribute the feature DTO may not carry, defaulting to `unknown`
so the model reasons from title/intent/work type), runs the classifier, and
returns `{change_request_id, route, confidence, rationale}`. It is exposed as
`POST /workboard/change-requests/{id}/classify-route` in `webapp.py`,
reusing the same Doc-Registry key hydration as the gate routes. The
endpoint commits nothing — it is a suggestion the UI renders for human
confirmation.

The human confirms via the work-item dialog: **Quick handoff** drives the
existing quick lane (`POST .../context-pack` with `quick_mode=true`, see below),
**Full planning** keeps the normal PRD→Spec→FE/BE/QA flow.

### Context-pack endpoint — quick lane (FR-6.3)

`POST /workboard/change-requests/{id}/context-pack` accepts an optional body
`{quick_mode?: bool, source_evidence?: string}` and passes it through to
`generate_context_pack(..., quick_mode=..., source_evidence=...)`. Omitting the
body (or sending `{}`) preserves the full flow (`quick_mode=false`,
`status="draft"`, full PRD/spec/tasks bundle). `quick_mode=true` produces the
approved quick-mode Context Pack — `status="approved"`, no full artifact bundle,
a Quick Handoff Note + Source Evidence section, empty `lead_artifact_id`, no
auto-start — the human-confirmed quick lane. Acceptance criteria render from
both the quick-work string-array shape and the richer UI-managed object shape.

### Tracker handoff (outbound) — Linear issue creation

Outbound tracker issue creation is owned by Doc Registry, which holds the
encrypted credential and verifies the inbound Linear webhook:
`POST /workboard/change-requests/{id}/handoff-tracker` with body
`{integration_id}` creates the issue and returns `{identifier, url}`. The
decrypted token never leaves the Doc Registry process. See the "Tracker handoff
contract" in `/docs/contracts.md`. The agent is not in this path.

### Board plumbing routes

Additional endpoints in `webapp.py` that do not have dedicated subsections above:

| Route | Purpose |
|-------|---------|
| `POST /workboard/quick-work-item` | Quick-route CR creation from IDE issue content. Body: `{title, description, issue_url?, issue_key?, feature_key?, feature_name?, created_by?, workspace_id?, acceptance_criteria?: string[]}`. `created_by` and `workspace_id` are cooperative CLI attribution, not auth. When `acceptance_criteria` is provided, the trimmed list is used as-is. Otherwise the service auto-drafts 3–8 acceptance criteria from the description when a model is configured, or falls back to one generic AC. When `feature_key` is supplied, it upserts that Feature idempotently and links the CR; when omitted, the quick CR stays featureless. It creates a `work_type=bug_fix` CR and generates a quick context pack (`quick_mode=True`) so the `delivery_pack` gate passes. Returns `{change_request_id, change_request_key, context_pack_uri, acceptance_criteria, phase}` plus `feature_id`/`feature_key` only for feature-backed quick work. Context pack generation failure is non-fatal (phase returns `"intake"` instead of `"handoff"`). |
| `POST /governance/threads/{thread_id}/title` | Generate/refresh a thread title from the thread's messages via the intent classifier. |

## 9. Capability surface

The governance-chat node runs on a **dedicated support model** (`build_governance_ops_model`) — intentionally separate from the main governance model — with the fixed governance tool list, no per-node overlays, and no drafting tools. Using the same model as the coding agent that produced the artifact would introduce evaluator bias; the governance-ops model must be independent. Its API key is set with `GOVERNANCE_OPS_API_KEY` and passed to LangChain as the generic `api_key` model parameter.

**Governance-ops model config.** Set via env vars: `GOVERNANCE_OPS_MODEL` (default `gpt-5.4-mini`), `GOVERNANCE_OPS_MODEL_PROVIDER` (default `openai`), `GOVERNANCE_OPS_API_KEY`, and `GOVERNANCE_OPS_THINKING_LEVEL` (default `low`). Model selection is env-only and is not hydrated from Doc Registry settings. `GOVERNANCE_OPS_API_KEY` is passed to LangChain as `api_key`. `specgate model set` configures the separate main governance model for gates, classifiers, readiness, delivery review, and summaries. `low` reasoning keeps the support surface fast and cheap; raise to `medium`/`high` only if answer quality on complex gate explanations is insufficient.

**Main governance reasoning effort.** `build_model()` applies the configured `governance.default_thinking_level` (`low` / `medium` / `high`, default `low`) to the main model, mapped to each provider's native control: OpenAI `reasoning_effort` (gpt-5 models), Gemini `thinking_budget` (`low` = 0 so tokens stream as generated — the no-regression default — medium/high raise the budget), Anthropic extended-thinking `budget_tokens` (medium/high; `low` disables it), and OpenRouter's first-class `reasoning.effort` parameter. Providers/models without reasoning support no-op. The level is read from settings (`provider_keys.governance_thinking_level`) and a caller may override per build.

- **MCP** is narrowed to Doc Registry (artifact/context reads). Repo reading is IDE-side, and implementation evidence flows back through doc-registry tracker webhooks.
- **Skills** are a Doc Registry registry surfaced to IDEs via the `specgate://skills` MCP resource; the readiness gates use fixed prompts, with no agents-side per-node skill resolver. Wiring skills into gates as portable policy is a separate slice.

## 11. Multi-language + LLM-driven control flow

User input arrives in any language. **No keyword routing, no rule-based classification, no heuristics over user content.** All intent / routing / entity extraction goes through LLM classifiers with structured output (see `governance/llm_structured.py::structured_output_ainvoke` and `governance/quality_gates/judge.py` for the canonical pattern). Prompts and enums live in code; the decision lives in the model.

Allowed pattern matching (not user content): structural code keys — event names, status enums, internal node ids, env-var names, MCP tool names, JSON schema keys.

## 12. Eval contract (fake-judge baseline)

This module has a CI-safe eval contract baseline in `agents/evals/eval_contract.py`:

- `EvalCase`: `case_id`, `suite`, `input`, `expected`
- `EvalInput`: `artifact_md`, `knowledge_snippets[]`, `feedback_events[]`
- `EvalExpectation`: `verdict`, `confidence_min`
- `EvalResult`: `case_id`, `suite`, `actual`, `expected`, `score`, `passed`, `diagnostics`

Fake-judge runner:

- Datasets: `agents/evals/datasets/*_contract.jsonl`
- Command: `uv run python -m evals.run --target all --contract-judge fake --json`

This baseline keeps contract scoring deterministic in CI while still allowing
live calibration through `--contract-judge live`.

Launch gates for eval-dependent surfaces are defined in `agents/evals/README.md` ("Launch gate policy") and are required before enabling gate-judged product behavior.

## 13. Error handling

- Doc Registry / MCP calls — the registry client retries transient HTTP failures and re-raises the last error; a failed governance op surfaces to its caller (the chat tool result or the HTTP route) rather than being swallowed.
- Provider key missing / classifier failure → safe fallback classification.

## 14. Observability

- LangSmith tracing on by default; `custom_metadata.thread_id` carries the cross-frame correlation key
- The governance-ops HTTP routes (`webapp.py`) are each wrapped with the `_traced` helper so every governance op (readiness, delivery review, classification) is a root LangSmith `chain` run (tagged `governance`, `agent-api`) that nests the LLM/MCP work it triggers, with the route args as run inputs.
- Sentry breadcrumbs (optional, `SENTRY_DSN`) for Doc-Registry / MCP / provider errors
- Durable per-thread state (LangGraph `values`) is the persistable audit trail — `state.thread_title` and the standard `messages` channel carry every UI signal that survives reload

## 15. Env flags

The `governance` graph is a single governance-chat node — there are no sub-agents to enable or disable.

| Flag | Effect |
|------|--------|
| `DOC_REGISTRY_BASE_URL` | Required. Live Doc Registry endpoint used by the registry client + provider-key hydration |

## 16. References

- Governance node contract: `docs/governance/docs/spec.md`
- Event contract: `docs/governance/docs/event-contract.md`
- Streaming/debugging checklist: `../../../docs/testing.md`
- Deep Agents: <https://docs.langchain.com/>
