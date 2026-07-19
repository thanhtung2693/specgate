# Spec - Agent Module

**Status:** current
**Document type:** reference

## 1. Scope

The agent module exposes one LangGraph governance-chat graph plus FastAPI
governance-ops routes. Durable state lives in Doc Registry. Model-backed
operations are invoked as plain async Python services from `webapp.py`; they do
not route through the chat graph.

Layered docs:

- `docs/prd.md` - module intent;
- this file - module contract;
- `docs/governance/docs/spec.md` - governance subgraph details.

## 2. Inputs

- LangGraph `messages` for governance chat.
- Artifact ids, Feature ids/keys, ChangeRequest ids, workspace ids.
- Artifact `policy_snapshot_json`, including enabled gates, required
  roles/topics/evidence, approval/evidence policy, and frozen
  `gate_definitions`.
- Exact team Skill rubric text/digests frozen into the artifact snapshot at
  publication; the current Skill catalog is not runtime authority.
- Workboard state, Knowledge search results, delivery feedback, user-reported
  check evidence, repository observations, and optional Linear handoff signals
  from Doc Registry.

## 3. Outputs

- Artifact readiness and quality-gate verdicts.
- Quick-route work item creation results.
- Delivery-review verdicts and per-criterion details.
- Governance-chat answers and LangSmith traces.

## 4. Runtime Topology

```text
LangGraph governance graph              FastAPI app (webapp.py)
governance_chat node (read-only)  ->    readiness / llm-gates
create_agent(...)                 ->    delivery review
four runtime-scoped read tools     ->    acceptance-criteria drafting
no drafting subagents             ->    quick work / thread title
```

`governance_chat.py:graph` is the only graph. It uses
`build_governance_ops_model()` and LangChain's `create_agent` with a fixed
governance tool list. The compiled tool node contains exactly those four
read-only tools; it has no filesystem, shell, task/subagent, or todo tools.
There are no drafting sub-agents, HITL drafting interrupts, or per-node overlay
systems.
Governance-chat tools are pinned to the LangGraph runtime workspace context; a
missing workspace or a runtime/thread workspace mismatch fails closed before
Doc Registry is called. The graph never invokes readiness or other stateful
operations; callers run those through the explicit CLI/IDE workflow.

The alpha local appliance starts this graph through `langgraph dev`, so its
chat checkpoints are process-lifetime only. Durable records belong to Doc
Registry; users must not rely on appliance chat threads surviving a restart.

## 5. Governance Operations

`webapp.py` owns the HTTP route layer. Core services live under `board/` and
`quality_gates/`.

| Operation | Main code | Contract |
| --- | --- | --- |
| Artifact readiness | `board/quality_gates.py`, `quality_gates/judge.py` | Runs policy-enabled gates and persists artifact gate runs |
| Work-item gates | `board/quality_gates.py` | Runs CR quality gates and persists `gate_runs` |
| Quick work item | `webapp.py` + Doc Registry client | Creates CR, AC rows, and quick handoff when possible |
| Delivery review | `quality_gates/delivery_review.py`, `board/delivery_review.py` | Judges built result and persists `delivery_review`; an Agent-reported (`agent_attested`) pass requires a valid bound peer review or human review, while an all-bound locally reproduced result records `deterministic_checks` |
| Thread title | `webapp.py` | Generates title from thread messages after validating the trusted workspace |

Doc Registry is the source of truth. Agents code must use the REST API; it must
not couple to Doc Registry database, S3, or pgvector internals.

## 6. Readiness and Quality Gates

Readiness uses the artifact policy snapshot:

- `enabled_gates` selects gates;
- `required_roles` creates deterministic missing-role signal;
- `required_topics` drives completeness prompts and rollup;
- `gate_definitions` supplies the frozen gate version and exact Skill rubric
  content/digest; `gate_skills` remains navigation metadata;
- `required_evidence` is delivery-review policy, not readiness input.

`quality_gates/profile_snapshot.py::parse_profile_snapshot` treats an empty
snapshot as "no persisted policy yet" and otherwise accepts only the explicit
`specgate.policy/v1` envelope. That envelope requires
`approval_policy=human_required` and a supported `evidence_policy`; missing or
unsupported values are invalid. Missing, legacy, or unknown schema versions
fail closed with public
`compatibility_error="unsupported_policy_snapshot_version"`; malformed
non-empty JSON fails closed with
`compatibility_error="invalid_policy_snapshot"`. Artifact readiness and
ChangeRequest/work-item gate operations return no runs and do not call their
readiness/gate-run refresh endpoints for either compatibility failure.

Gate input is role-routed. Required sections are passed to each gate instead of
dumping every document into every prompt. Gate verdicts carry `state`, `hint`,
`confidence`, and short evidence. Low-confidence `pass` and low-confidence
`fail` become `needs_human_review`; `warn` and `not_applicable` pass through.

Unresolved gate verdicts (`warn`, `fail`, `needs_human_review`) ride the Context
Pack as "Unresolved Quality Gates" so an execute-anyway handoff keeps the gaps.
`spec_repo_drift` is an artifact-scoped readiness run (not a CR gate_run), so the
pack pulls it from the source artifact's readiness runs and lists each finding
(`doc_path`/`conflicting_claim`/`spec_section`) as sub-bullets, parsed from the
run's `evidence_json` findings envelope, so the drifted-doc guidance reaches the
next agent.

## 7. Knowledge and Attachments

Governance chat exposes `search_governance_knowledge`, a read-only tool backed
by Doc Registry `POST /governance/context/search`. Model-controlled arguments
are `query`, optional linked Feature/ChangeRequest ids, document types,
authority levels, `limit`, and `context_mode`. `workspace_id` is not a tool
argument; it is injected from trusted LangGraph runtime/thread context.

Knowledge chunks are untrusted quoted reference material. Prompts must delimit
them as references. Knowledge may be cited or compared against approved
artifacts, but it must not override system, developer, approved artifact, gate
contract, or delivery-review instructions. Governance-chat answers cite every
Knowledge-grounded material claim with returned `specgate://knowledge/...`
citations, surface conflicts instead of silently resolving them, and distinguish
no result, unavailable embeddings, and retrieval failure.

Knowledge search must come from explicit tool planning or model-decided flow.
No keyword/language heuristic over user text may trigger search. `workspace_id`
is a structural field from caller/session state, never extracted from prose.

Feature attachments are audience-gated:

| Audience | Consumer |
| --- | --- |
| `gate` | Quality gates |
| `coding_agent` | Context Pack |
| `both` | Both |

File/image attachments render through Doc Registry content proxy URLs, not S3
URLs. IDE-agent local uploads are not automatic attachments; contract-changing
material must be published as a new artifact version.

## 8. Quick Work and Context Pack

`POST /workboard/quick-work-item` creates quick-route CRs from issue content.
When caller supplies acceptance criteria, trimmed caller text wins. Rich criteria
may carry human-authored `verification_binding`.

When acceptance criteria are absent, the settings-backed governance model may
draft them. If no model can draft criteria, request fails and asks caller to
provide criteria or configure a model. The service must not create generic
fallback criteria or infer requirements that are absent from the supplied
intent. Sparse input that cannot support concrete criteria is rejected.

Feature behavior:

- supplied `feature_key` upserts/links a Feature;
- omitted `feature_key` keeps the CR featureless;
- quick work returns `ready` only with effective acceptance criteria; an empty
  model draft is rejected before it creates a ChangeRequest. Doc Registry
  derives its lightweight Context Pack from the persisted ChangeRequest and
  acceptance criteria on read. It does not create an unreviewed artifact.

All custom WorkBoard and artifact-readiness agent routes and their callable
`board/` services require a non-empty `workspace_id`. Quick-work creation, gate
execution, and delivery review forward that value to
every WorkBoard/artifact/Skill lookup and mutation.
Missing workspace context is rejected before model or Doc Registry work; the
registry remains the final isolation boundary.

Context Pack reads belong only to Doc Registry's versioned CLI API. This avoids
a second, agent-service handoff implementation and keeps IDE-agent pickup on
one workspace-scoped surface.

User-authored model inputs are deterministically bounded before inference:
artifact text is capped at 48,000 characters, frozen gate rubrics at 8,000,
delivery criteria at 16,000, and completion reports at 48,000. Governance-chat
artifact-document tool output is capped at 12,000 characters per document and
32,000 across the returned bundle. Chat history is summarized before a model
call when it reaches about 16,000 tokens, retaining about 6,000 recent tokens.
Before every governance-chat model call, each individual user message is copied
into a model-only representation capped at 32,000 characters. The copy
preserves its beginning and conclusion and includes
`[SpecGate truncated oversized model input]`; the source transcript remains
unchanged. Other truncation is likewise disclosed in model input and does not
alter stored artifacts or evidence.

## 9. Delivery Review

Delivery review judges built result against canonical acceptance criteria,
coding-agent completion feedback, checks, affected files, and evidence.
Completion claims use the exact `satisfied`, `partial`, and `not_done` values
and bind only through canonical criterion IDs. Text equality and claim prefixes
are not identity or status aliases.

Per-criterion verdicts: `met`, `unmet`, `unclear`.

Compound criteria require evidence for every independently verifiable clause.
A generic file or test citation does not prove unasserted visual, manual,
accessibility, theme, or device-smoke clauses; uncovered clauses resolve to
`unclear`. Review output always uses canonical acceptance-criterion row IDs. A
foreign or missing model-emitted ID becomes an `unclear` row under the canonical
ID rather than disappearing or being persisted as a synthetic identifier.

Deterministic binding:

- AC `verification_binding` names a check in `checks[]`;
- matching `pass` -> `met`;
- matching `fail` -> `unmet`;
- missing or `skipped` -> `unclear`;
- bound criteria are resolved before model judging and are not shown to the LLM.

Overall verdict:

- `pass` only when every criterion is met and no check failed;
- failed check or unmet criterion -> `fail`;
- unclear criterion or no criteria -> `needs_human_review`;
- low-confidence model result may downgrade through shared confidence policy.
- `corroborated_required` still routes model/claim-only passes to
  `needs_human_review`, but a pass where every criterion resolved through a
  canonical deterministic binding is treated as corroborated.
- A merged-PR/MR event corroborates only when its normalized `head_sha` equals
  the latest completion receipt's normalized `git_receipt.head_revision`.
  Missing, mismatched, and stale merge events do not corroborate. Users' own
  test and CI results remain cited or locally reproduced evidence, not a
  repository-observation assurance source.

When every canonical criterion has a verification binding, the no-model path
derives the review entirely from locally reproduced named checks, records
`judge_model=deterministic_checks`, and describes that source directly. Mixed
bound and claim-based reviews retain the weaker `agent_attested` authority.

`board/delivery_review.py::review_change_request_delivery` loads canonical ACs,
finds the newest `coding_agent.completed` event, runs review, hydrates missing
criterion text from the CR, and persists `delivery_review` via Doc Registry gate
refresh. On `fail` or `needs_human_review`, Doc Registry Context Pack readback
folds outstanding feedback into the next handoff.

Artifact-backed delivery review fails closed when its lead/canonical artifact
or frozen policy snapshot cannot be loaded. The service persists
`needs_human_review` with reason `policy_unavailable`; it does not call a model
or derive a verdict from agent claims under a weaker policy. A persisted
`specgate.policy/v1` snapshot without `evidence_policy` is invalid. Quick-route
bug-fix work with no lead artifact has no artifact snapshot by design and uses
the built-in `attested_ok` policy; other artifactless routes fail closed.

Every persisted delivery review, including the deterministic
`policy_unavailable` guard, records the exact
`completion_feedback_event_id`. That binding prevents a review of an older
completion from authorizing a later receipt.

Canonical ACs come from
`GET /workboard/change-requests/{id}/acceptance-criteria`, whose row IDs are the
same `criterion_id` values used by completion reports. Failure to retrieve those
rows fails review; the denormalized `acceptance_criteria_json` mirror cannot
preserve their IDs and is not a fallback.

When platform model/provider is unavailable, runner may derive the verdict from
agent claims and checks. Provider hints should preserve actionable upstream
detail without reflecting stack traces.

## 10. Capability Surface

Governance chat uses `GOVERNANCE_OPS_MODEL`,
`GOVERNANCE_OPS_MODEL_PROVIDER`, `GOVERNANCE_OPS_API_KEY`, and
`GOVERNANCE_OPS_THINKING_LEVEL`. This support model is separate from the
settings-backed governance model used for verdict-producing operations.

The chat graph binds four read-only diagnostic tools:

| Tool | Contract |
| --- | --- |
| `get_artifact` | Runtime-workspace-scoped artifact envelope read |
| `get_artifact_documents` | Runtime-workspace-scoped read of every artifact document, grouped only by its declared role; filenames do not select content |
| `list_artifact_readiness` | Runtime-workspace-scoped artifact readiness run list |
| `search_governance_knowledge` | Runtime-workspace-scoped Knowledge retrieval with canonical citations |

Server-side governance operations read model/provider settings from Doc Registry
where applicable. Reasoning effort maps to provider-native controls when
supported and no-ops otherwise.

## 11. Multi-language and Control Flow

User input may be any language. Intent, route, and entity extraction must go
through LLM classifiers with structured output. No keyword routing, no
rule-based classification, no heuristics over user content.

Allowed pattern matching is structural only: status enums, event names, route
ids, env vars, and JSON schema keys.

## 12. Eval Contract

CI-safe eval baseline lives in `agents/evals/eval_contract.py`.

Fixture-judge command:

```bash
uv run python -m evals.run --target all --contract-judge fixture --json
```

Live calibration may use `--contract-judge live`, but CI must keep deterministic
fixture coverage for gate-dependent behavior.

## 13. Errors and Observability

HTTP route errors:

- retryable Doc Registry writes may retry, then surface the last error;
- custom routes log exception type and stack server-side;
- external route responses use generic `502 {"detail":"<operation> failed"}`
  for provider/registry exceptions;
- missing model for route shortcut returns conservative output or validation
  error, depending on operation.

Observability:

- LangSmith tracing on by default;
- trace inputs and outputs are hidden by default through
  `LANGSMITH_HIDE_INPUTS=true` and `LANGSMITH_HIDE_OUTPUTS=true`; an operator
  must explicitly set either value to `false` before payload content is sent;
- route handlers use `_traced` root chain runs tagged `governance` and
  `agent-api`;
- `custom_metadata.thread_id` carries cross-frame correlation;
- LangGraph state values plus messages are the durable chat audit surface.

## 14. Environment

| Variable | Contract |
| --- | --- |
| `DOC_REGISTRY_BASE_URL` | Required Doc Registry base URL |
| `GOVERNANCE_OPS_MODEL` | Chat support model, default `gpt-5.4-mini` |
| `GOVERNANCE_OPS_MODEL_PROVIDER` | Chat support provider, default `openai` |
| `GOVERNANCE_OPS_API_KEY` | Chat support API key |
| `LANGSMITH_HIDE_INPUTS` | Hide trace input payloads; defaults to `true` |
| `LANGSMITH_HIDE_OUTPUTS` | Hide trace output payloads; defaults to `true` |
| `GOVERNANCE_OPS_THINKING_LEVEL` | Chat support thinking level, default `low` |

No sub-agent flags exist because graph has one governance-chat node.

## 15. References

- `docs/governance/docs/spec.md`
- `docs/governance/docs/event-contract.md`
- `../../../docs/contributing/testing.md`
