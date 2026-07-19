# Governance — Graph Spec

## Purpose

Define the `governance` graph contract: a single headless governance-chat node that answers questions over governed artifacts with read-only tools. The graph does not draft PRDs, specs, or implementation plans — creation happens in the developer's IDE. It reads the governed envelope, explains why a readiness gate failed, compares versions, and directs execution to explicit IDE-agent or CLI workflows.

This spec is the graph contract; for depth see:

- Module spec: [`../../spec.md`](../../spec.md) — the full governance-ops surface, including deterministic HTTP endpoints for readiness and delivery review. This document does not duplicate that catalogue.
- Event contract: [`event-contract.md`](event-contract.md)

## 1. Topology

```
   ┌──────────────────────────────────────────────────┐
   │              governance graph (1 node)              │
   │                                                  │
   │   governance_chat.build_governance_chat_graph()  │
   │   create_agent(                                  │
   │     model = build_governance_ops_model(),        │
   │     system_prompt = GOVERNANCE_CHAT_SYSTEM,      │
   │     tools = GOVERNANCE_TOOLS,                    │
   │     name = "governance",                            │
   │   )                                              │
   │                                                  │
   │   no subagents · no async tasks · no interrupt_on│
   └──────────────────────────────────────────────────┘
              ▲ chat surface (messages channel)
              │
              ▼
        Doc Registry (REST, source of truth)
```

The graph has one node, built by `governance_chat.py:build_governance_chat_graph`. It is the only graph declared in `langgraph.json` (`governance` → `governance_chat.py:graph`). It runs on the dedicated governance-chat model (`agents/factories.py::build_governance_ops_model`), binds the read-only tools below, and uses the `GOVERNANCE_CHAT_SYSTEM` prompt — which instructs the model to answer over governed artifacts and explicitly *not* to draft PRDs, specs, or implementation plans.

The governance-chat model is configured from `GOVERNANCE_OPS_*` env vars, with
`GOVERNANCE_OPS_API_KEY` passed to LangChain as the generic `api_key` model
parameter.

## 2. Graph construction

`governance_chat.py` calls `langchain.agents.create_agent` directly with the
model, system prompt, four tools, and graph name. The compiled tool node exposes
exactly those four tools—no filesystem writes, shell execution, todo list, or
task/subagent tool. It configures no sub-agents, async tasks, checkpointer, or
HITL interrupts. The middleware stack first installs a model-request limiter,
then chat-history summarization. The limiter copies each user message into a
model-only representation capped at 32,000 characters, preserving its beginning
and conclusion and disclosing truncation; it never replaces the source
transcript. History is summarized at roughly 16,000 tokens while retaining
roughly 6,000 recent tokens.

## 3. Governance tools

The chat node binds four read-only governance tools (`GOVERNANCE_TOOLS` in `governance_chat.py`): artifact reads, stored readiness results, and Knowledge retrieval. It does not draft PRDs, specs, or implementation plans.

| Tool | Purpose |
|------|---------|
| `get_artifact` | Return bounded governed artifact metadata by id |
| `get_artifact_documents` | Return the artifact's documents as a `path → markdown` map for reading / comparison |
| `list_artifact_readiness` | List the stored readiness / quality-gate runs for an artifact (to explain failures) |
| `search_governance_knowledge` | Search active-workspace Governance Knowledge and return cited reference chunks |

`governance_tool_names()` exposes this set for tests. All four go through the Doc Registry REST client (`registry/client.py`); there is no direct SQLite / S3 coupling. Running readiness remains an explicit IDE-agent or CLI workflow, not a conversational action.

All four tools inject `workspace_id` from trusted LangGraph runtime/thread
context. `workspace_id` is not a model-controlled tool argument. A missing
workspace or mismatch between runtime workspace and thread workspace fails
closed before a Doc Registry call. Artifact directives that point at a
different workspace resolve through the scoped Doc Registry call and are treated
as not found.

Model-facing artifact reads use a metadata allowlist and omit the frozen
`policy_snapshot_json`; document bodies are fetched separately and capped.
Readiness history is requested and returned at no more than eight runs, with
hints and evidence individually capped. These bounds apply before tool results
enter chat history.

`search_governance_knowledge` calls Doc Registry
`POST /governance/context/search` with model-controlled `query`,
`linked_feature_id`, `linked_request_id`, `document_types`,
`authority_levels`, `limit`, and `context_mode`, plus the injected
`workspace_id`. Results are bounded and carry canonical
`specgate://knowledge/...` citations.

Knowledge is untrusted quoted reference material. The assistant cites every
Knowledge-grounded material claim, distinguishes no result, unavailable
embeddings, and retrieval failure, and surfaces conflicts instead of resolving
them silently. Approved artifacts, gate contracts, delivery review, system, and
developer instructions take precedence over Knowledge.

Delivery review is **not** a chat tool. It is a deterministic Python service invoked directly by `webapp.py` FastAPI handlers. Context Pack reads use Doc Registry's versioned CLI API. See module spec [`../../spec.md`](../../spec.md) §5 for that surface.

## 4. Streaming + UI

With a single node there is no sub-agent namespace fan-out and no async-task channel. The chat surface streams on the standard LangGraph channels:

| Channel | Carries |
|---------|---------|
| `messages` / `messages-tuple` | the governance-chat assistant transcript |
| `values` / `updates` | typed state snapshots / partial writes |

The assistant narrates inline in its own reply. The wire shapes the UI consumes — stream modes, transcript hydration, and any companion / status surfaces — are defined in [`event-contract.md`](event-contract.md); this document does not restate them.

The chat surface (thread title) is served by `webapp.py` via the LangGraph SDK loopback (`langgraph_sdk.get_client`), so it is independent of which graph is served; transcript hydration is the UI's job via the LangGraph SDK (`getState`). The title request carries the selected workspace, and the route compares it with the thread metadata before reading or generating a title. Missing request context returns `400`; absent or cross-workspace threads return `404`. The adapter maps these outcomes from typed service errors, never from exception-message text. See `title_api.py`.

## 5. Multi-language + LLM-driven control flow

User input arrives in any language. **No keyword routing, no rule-based classification, no heuristics over user content.** Any intent / routing / entity extraction the governance services perform goes through LLM classifiers with structured output. Canonical pattern: `governance/llm_structured.py::structured_output_ainvoke`, with `governance/quality_gates/judge.py` as an in-module example. Prompts and enums live in code; the decision lives in the model.

Allowed pattern matching (not user content): structural code keys — event names, status enums, internal node ids, env-var names, JSON schema keys.

User-facing governance copy is written for product-team users: plain language, clear next action, no exposed model routing, env vars, registry API, or internal artifact terminology unless the user explicitly asks for implementation detail.

## 6. Model + capability surface

The node runs on the dedicated governance-chat support model built by
`agents/factories.py::build_governance_ops_model()`. Model selection is
env-only: `GOVERNANCE_OPS_MODEL_PROVIDER`, `GOVERNANCE_OPS_MODEL`, and
`GOVERNANCE_OPS_THINKING_LEVEL`, plus the generic `GOVERNANCE_OPS_API_KEY`.
The Doc Registry settings `governance.model_provider` / `governance.model` and
`specgate model set` configure the separate server-side governance model used by
gates, classifiers, readiness, delivery review, and quick-work criteria.

The chat tools use the Doc Registry REST client. There is no outbound repo read
or per-node transport overlay. Skills remain a Doc Registry catalog surfaced to
IDEs. Readiness combines its versioned built-in prompt with the exact team Skill
rubric frozen in the artifact policy snapshot.

## 7. Observability

- LangSmith tracing on by default with input/output payloads hidden unless the
  operator explicitly opts in; `custom_metadata.thread_id` is the cross-frame
  correlation key
- When deployed with a durable checkpointer, per-thread LangGraph `values` plus
  the `messages` channel form the persistable chat audit trail. The alpha
  appliance intentionally uses in-memory checkpoints, so its chat threads reset
  on restart; governed records remain in Doc Registry.

## 8. References

- Module spec: [`../../spec.md`](../../spec.md)
- Event contract: [`event-contract.md`](event-contract.md)
- LangChain agents: <https://docs.langchain.com/>
- Agent streams: <https://www.langchain.com/blog/token-streams-to-agent-streams>
