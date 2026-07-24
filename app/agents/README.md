# SpecGate Agents — Governance Ops

LangGraph-based governance operations service for the SpecGate monorepo. See
[`docs/spec.md`](docs/spec.md) for the module contract and
[`docs/governance/docs/spec.md`](docs/governance/docs/spec.md) for the
governance-chat graph contract.

## Prerequisites

- Python 3.12+
- [uv](https://docs.astral.sh/uv/)
- Doc Registry running if publishing (`app/doc-registry/`)

## Setup

```bash
cd app/agents
cp .env.example .env
uv sync --all-groups
```

Configure models:

- Main server-side governance workloads (gates, readiness,
  delivery review, and quick-work criteria) use the provider/model stored in
  Doc Registry settings or configured with `specgate model set`.
- The experimental LangGraph governance-ops chat agent uses env-only model
  configuration: `GOVERNANCE_OPS_MODEL_PROVIDER`, `GOVERNANCE_OPS_MODEL`,
  `GOVERNANCE_OPS_API_KEY`, and `GOVERNANCE_OPS_THINKING_LEVEL`. OpenRouter
  model ids are `vendor/model` slugs.

Design reference links and uploads are captured in the Context Pack, but are not
inspected by the agent.

## Run locally

### Two modes

- **FastAPI only (no chat).** Serves just the HTTP webapp
  (readiness, acceptance-criteria drafting, delivery review) — no chat agent:

  ```bash
  uv run uvicorn specgate_agents.governance.webapp:app --host 0.0.0.0 --port 2024
  ```

- **LangGraph (adds the experimental governance-ops chat agent).** Runs the full
  `langgraph.json` (the `governance` chat graph plus the same webapp as `http.app`)
  via the LangGraph CLI / server, as below. The chat agent remains optional and
  experimental; FastAPI-only mode exposes governance operations but no chat.

### Fast loop (in-memory checkpoints, with chat)

```bash
uv run langgraph dev
```

Default API: `http://127.0.0.1:2024` (see LangGraph CLI docs). Checkpoints and
thread metadata live in memory and disappear when the process stops. Long dev
sessions with large agent state (markdown fields, message history) can push
memory high — restart the server or prune threads when that happens.

### Work with the local appliance

Run `make setup` from the repository root to start the complete Full-mode
backend in one container. For native Agents iteration, point
`DOC_REGISTRY_BASE_URL` at
`http://localhost:3000/api/doc-registry` (or the selected appliance port), then
run `uv run langgraph dev`. Rebuild the embedded appliance with `make build &&
make up` before validating the packaged runtime.

The root `docker-compose.yml` is reserved for separable self-host/cloud
deployment validation. It is not part of the normal local development loop.

### Prune all threads

With the LangGraph API running:

```bash
node scripts/prune-langgraph-threads.mjs
node scripts/prune-langgraph-threads.mjs --dry-run   # list only
```

Stop `langgraph dev` before switching to Postgres. Its in-memory state is not
migrated.

### LangSmith

Enable tracing with `LANGCHAIN_TRACING_V2=true` or
`LANGSMITH_TRACING_V2=true` and `LANGCHAIN_API_KEY` / `LANGSMITH_API_KEY` (see
LangSmith docs). Trace input and output payloads are hidden by default, including
automatic LangGraph traces. An operator who intentionally wants payload content
in LangSmith must explicitly set `LANGSMITH_HIDE_INPUTS=false` and
`LANGSMITH_HIDE_OUTPUTS=false`.

## Tests

```bash
uv run pytest
uv run ruff check src tests
uv run ruff format --check src tests
uv run deptry src evals
```

The default `pytest` run covers routing, wiring, governance tools, and mocked
integration checks. Run the external-service smoke checks only when you
intentionally want live LLM/LangSmith coverage:

```bash
GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke tests/test_live_smoke_governance.py -q
```
