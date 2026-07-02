# Governance — Graph Spec

## Purpose

Define the `governance` graph contract: a single headless governance-chat node that answers questions over governed artifacts and runs a curated set of governance operations as tools. The graph does not draft PRDs, specs, or implementation plans — creation happens in the developer's IDE. It reads the governed envelope, explains why a readiness gate failed, compares versions, surfaces conflicts, and can trigger a profile-scoped readiness run on request.

This spec is the graph contract; for depth see:

- Module spec: [`../../spec.md`](../../spec.md) — the full governance-ops surface, including the deterministic HTTP endpoints (readiness, delivery review, route classification, context-pack). This document does not duplicate that catalogue.
- Event contract: [`event-contract.md`](event-contract.md)

## 1. Topology

```
   ┌──────────────────────────────────────────────────┐
   │              governance graph (1 node)              │
   │                                                  │
   │   governance_chat.build_governance_chat_graph()  │
   │   build_supervisor(                              │
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

The graph has one node, built by `governance_chat.py:build_governance_chat_graph`. It is the only graph declared in `langgraph.json` (`governance` → `governance_chat.py:graph`). It runs on the dedicated governance-chat model (`agents/factories.py::build_governance_ops_model`), binds the governance read/run tools below, and uses the `GOVERNANCE_CHAT_SYSTEM` prompt — which instructs the model to answer over governed artifacts and explicitly *not* to draft PRDs, specs, or implementation plans.

The governance-chat model is configured from `GOVERNANCE_OPS_*` env vars, with
`GOVERNANCE_OPS_API_KEY` passed to LangChain as the generic `api_key` model
parameter.

## 2. Supervisor factory

`main_agent.py::build_supervisor` is the generic deep-agent wrapper (a thin pass-through over deepagents `create_deep_agent`). It *can* take `subagents`, `async_subagents`, and `interrupt_on` HITL gates, and is reused that way elsewhere. **The `governance` graph passes none of them** — no sub-agents, no async tasks, no HITL interrupts. Readers should not infer a drafting topology from the factory's broader signature: governance-chat supplies only `model`, `system_prompt`, `tools`, and `name`.

## 3. Governance tools

The chat node binds four governance tools (`GOVERNANCE_TOOLS` in `governance_chat.py`). They are governance-ops operations — artifact reads and readiness runs — but **no drafting** (no PRD, spec, or implementation-plan authoring):

| Tool | Purpose |
|------|---------|
| `get_artifact` | Return the governed artifact envelope (status, version, role-tagged files) by id |
| `get_artifact_documents` | Return the artifact's documents as a `path → markdown` map for reading / comparison |
| `list_artifact_readiness` | List the stored readiness / quality-gate runs for an artifact (to explain failures) |
| `run_artifact_readiness` | Run the profile-scoped readiness gates for an artifact and return the verdicts |

`governance_tool_names()` exposes this set for tests. All four go through the Doc Registry REST client (`registry/client.py`); there is no direct SQLite / S3 coupling. `run_artifact_readiness` delegates to the same `board/quality_gates.py::run_llm_gates_for_artifact` path the HTTP readiness endpoint uses, so a chat-triggered run and an HTTP-triggered run share one implementation.

The broader governance operations — delivery review, route classification, context-pack compilation — are **not** chat tools. They are deterministic Python services invoked directly by `webapp.py` FastAPI handlers. See module spec [`../../spec.md`](../../spec.md) §5 for that surface.

## 4. Streaming + UI

With a single node there is no sub-agent namespace fan-out and no async-task channel. The chat surface streams on the standard LangGraph channels:

| Channel | Carries |
|---------|---------|
| `messages` / `messages-tuple` | the governance-chat assistant transcript |
| `values` / `updates` | typed state snapshots / partial writes |

The assistant narrates inline in its own reply. The wire shapes the UI consumes — stream modes, transcript hydration, and any companion / status surfaces — are defined in [`event-contract.md`](event-contract.md); this document does not restate them.

The chat surface (thread title) is served by `webapp.py` via the LangGraph SDK loopback (`langgraph_sdk.get_client`), so it is independent of which graph is served; transcript hydration is the UI's job via the LangGraph SDK (`getState`). See `title_api.py`.

## 5. Multi-language + LLM-driven control flow

User input arrives in any language. **No keyword routing, no rule-based classification, no heuristics over user content.** Any intent / routing / entity extraction the governance services perform goes through LLM classifiers with structured output. Canonical pattern: `governance/llm_structured.py::structured_output_ainvoke`, with `governance/quality_gates/judge.py` as an in-module example. Prompts and enums live in code; the decision lives in the model.

Allowed pattern matching (not user content): structural code keys — event names, status enums, internal node ids, env-var names, MCP tool names, JSON schema keys.

User-facing governance copy is written for product-team users: plain language, clear next action, no exposed model routing, MCP, env vars, registry API, or internal artifact terminology unless the user explicitly asks for implementation detail.

## 6. Model + capability surface

The node runs on the dedicated governance-chat support model built by
`agents/factories.py::build_governance_ops_model()`. Model selection is
env-only: `GOVERNANCE_OPS_MODEL_PROVIDER`, `GOVERNANCE_OPS_MODEL`, and
`GOVERNANCE_OPS_THINKING_LEVEL`, plus the generic `GOVERNANCE_OPS_API_KEY`.
The Doc Registry settings `governance.model_provider` / `governance.model` and
`specgate model set` configure the separate server-side governance model used by
gates, classifiers, readiness, delivery review, and summaries.

MCP is narrowed to Doc Registry reads — there is no outbound repo-read MCP (repo reading is IDE-side) and no per-node MCP overlay. Skills remain a Doc Registry registry surfaced to IDEs via the `specgate://skills` MCP resource; the readiness gates use fixed prompts.

## 7. Observability

- LangSmith tracing on by default; `custom_metadata.thread_id` is the cross-frame correlation key
- Sentry breadcrumbs (optional, `SENTRY_DSN`)
- Durable per-thread state (LangGraph `values`) plus the `messages` channel is the persistable audit trail

## 8. References

- Module spec: [`../../spec.md`](../../spec.md)
- Event contract: [`event-contract.md`](event-contract.md)
- Deep Agents: <https://docs.langchain.com/>
- Agent streams: <https://www.langchain.com/blog/token-streams-to-agent-streams>
