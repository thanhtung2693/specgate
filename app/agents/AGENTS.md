# agents — Agent Rules

Extends root [../AGENTS.md](../AGENTS.md). Read that first; this file only adds module-specific conventions.

## Current state

**Governance-ops docs and runnable service** live in this directory: product and graph
contracts under `docs/`, Python package under `src/specgate_agents/`. Run the LangGraph
service with `uv sync` and `uv run langgraph dev` (see [README.md](README.md)). See
`docs/README.md` for the intended doc set (`prd.md`, `spec.md`) and
`docs/governance/docs/spec.md` for the governance-chat graph contract.

When changing governance-ops behavior, read `docs/spec.md` and
`docs/governance/docs/spec.md` alongside the code in `src/specgate_agents/governance/`.

## Package layout

```
src/specgate_agents/governance/
├── governance_chat.py   # Thin single-node governance-chat agent (langgraph `governance`)
├── main_agent.py        # build_supervisor — generic deep-agent wrapper (reused by governance_chat)
├── webapp.py            # FastAPI governance-ops HTTP endpoints (gates, readiness, ...)
├── agents/factories.py  # Chat-model builders: build_model + build_governance_ops_model
├── intent/              # Thread-title classifier
├── board/               # Governance routes: context-pack, gates, route/delivery review, quick work
├── quality_gates/       # Readiness gate harness (judge, completeness, delivery_review)
├── registry/            # Doc Registry HTTP client
├── title_api.py         # Thread-title generation (chat surface)
└── ...
```

- The only LangGraph graph is `governance` → `governance_chat.py:graph`: one node, the system model, governance-ops as tools (read artifact, list/run readiness), no PRD/spec drafting and no HITL. Governance operations are exposed as `webapp.py` HTTP endpoints.
- The chat surface is thin and domain-specific (explain a gate failure, compare versions, surface conflicts). It must not compete with IDE agents for open-ended repo work.

## Conventions

- **Python ≥ 3.12**, managed with `uv` (package + venv).
- **LangGraph** for agent orchestration (graph nodes, state, tool bindings).
- Design references are stored as artifact metadata and surfaced through Doc
  Registry; the agent does not inspect external design tools directly.
- Tests with `pytest`; follow root [../AGENTS.md](../AGENTS.md) for TDD and verification expectations.
- Type hints throughout; `ruff` for lint + format.
- Secrets via env vars, never hardcoded. LangGraph state must not leak credentials into traces.
- When the agent publishes to Doc Registry, use the REST API in `doc-registry/docs/spec.md` §6 — do not couple to SQLite / S3 directly.

## Multi-language, LLM-driven control flow

This is a **multi-language SDLC agent** — user input arrives in Vietnamese, English, or mixed, and may use any phrasing, abbreviation, or domain shorthand. Coding agents working on this module **must not** introduce:

- **Hardcoded keywords** over user text — no `if "draft" in user_text`, no `["create", "tạo", "make"]` lookup tables, no fixed phrase lists.
- **Rule-based classification** — no decision trees, regex routing, or string-pattern dispatch over user-supplied content for intent, routing, or entity extraction.
- **Heuristics over content** — no length thresholds, capitalization rules, punctuation-based parsers, or language detection used as a control-flow gate.

Use LLM classification with structured output (see `src/specgate_agents/governance/llm_structured.py` (`structured_output_ainvoke`) for the canonical helper, and `src/specgate_agents/governance/intent/title.py` for an in-module classifier example). Prompts and enums live in code; the *decision* lives in the model.

Allowed pattern matching (not user content): structural code keys (event names, status enums, internal node ids, env var names, MCP tool names, JSON schema keys). The rule applies to anything the user typed or that was generated as user-facing natural language.

If you find yourself writing pattern matching against user text, stop and route through an LLM-backed classifier instead. If a classifier seems too heavy, that is a signal the prompt or the routing graph needs work — not a license for keyword matching.

## Docs hygiene

- Keep `docs/prd.md` aligned with intent (why this module exists).
- Keep `docs/spec.md` aligned with graph / node / state / tool contract (what it does).
- `docs/governance/docs/spec.md` is a nested spec for the governance-chat graph — mirror this pattern for other subgraphs (`docs/<subgraph>/docs/spec.md`).
- Cross-module contracts (e.g. artifact publish payload, event consumption) belong in `/docs/contracts.md` — update there, not only here.
- For any code change, update the nearest relevant doc in the same change; do not defer docs to a later pass.

## Test markers

- `live_smoke` — opt-in; calls a real external service (LangSmith). Run with
  `GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke`. The default `uv run pytest`
  excludes it; everything else runs by default.

## Verification

- Run the relevant `agents` verification commands before claiming completion.
- If a change affects governance-ops behavior, verify against both code and the layered docs.
- If full verification cannot run, say exactly what was not run and why.
- For backend changes that affect streaming (custom events, reply tokens,
  tool/disclosure surfaces), follow the SSE probe + container reload checklist
  in [../docs/testing.md §LangGraph Streaming SSE Probe](../docs/testing.md#langgraph-streaming-sse-probe-runsstream).
  `docker restart agents-langgraph-api-1` does NOT pick up new Python source —
  rebuild + recreate the container, or use `langgraph up --watch` / native
  `langgraph dev`, before re-testing.
