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
- The LangGraph governance-ops chat agent uses env-only model
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

- **LangGraph (adds the governance-ops chat agent).** Runs the full
  `langgraph.json` (the `governance` chat graph plus the same webapp as `http.app`)
  via the LangGraph CLI / server, as below. The chat agent is optional;
  FastAPI-only mode exposes governance operations but no chat.

### Fast loop (in-memory checkpoints, with chat)

```bash
uv run langgraph dev
```

Default API: `http://127.0.0.1:2024` (see LangGraph CLI docs). Checkpoints and
thread metadata live in memory and disappear when the process stops. Long dev
sessions with large agent state (markdown fields, message history) can push
memory high — restart the development server when that happens. Restarting
discards its ephemeral chat history; durable governance records remain in Doc
Registry.

### Work with the local appliance

Run `make setup` from the repository root to start the complete Full-mode
backend in one container. For native Agents iteration, point
`DOC_REGISTRY_BASE_URL` at
`http://localhost:3000/api/doc-registry` (or the selected appliance port), then
run `uv run langgraph dev`. Rebuild the embedded appliance with `make build &&
make up` before validating the packaged runtime.

The root `docker-compose.yml` is reserved for separable self-host/cloud
deployment validation. It is not part of the normal local development loop.
Its agents image uses the same frozen production `uv.lock` as the appliance,
with `langgraph`, `uvicorn`, and `python` on `PATH` from the installed virtual
environment. `uv` is build-only, not a runtime dependency; command overrides can
invoke `uvicorn specgate_agents.governance.webapp:app` directly.

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

Dependency upgrades must preserve a compatible runtime, not merely maximize
each transitive version. The tested LangGraph API floor is 0.15.1. Its
Prometheus exporter requires OpenTelemetry SDK/API 1.42.1, which conflicts
with FastAPI 0.142.2's OpenTelemetry API minimum of 1.44.0; the lock therefore
retains FastAPI 0.141.1. Recheck upstream constraints before lifting this limit.
Do not use `--no-deps` or prerelease runtime builds to bypass it. After changing
the lock, rerun the tests above, build a wheel, and smoke-test `langgraph dev`
with isolated config/state and tracing disabled before invoking live providers.

Inspect proposed lock changes before applying a blanket upgrade: resolving the
whole graph can downgrade a model integration to satisfy unrelated transitive
constraints. Prefer targeted upgrades that retain the tested provider major.
The current Google GenAI SDK caps Tenacity below 9.2, and LangGraph API caps
gRPC below 1.82, Protobuf below 7, JSONSchema-RS below 0.45, and Structlog below
26. These are upstream compatibility constraints, not independent latest-version
targets. Its Prometheus exporter explicitly requires a beta release; keep that
existing dependency constraint without opting other packages into prereleases.

The default `pytest` run covers routing, wiring, governance tools, and mocked
integration checks. Run the external-service smoke checks only when you
intentionally want live LLM/LangSmith coverage:

```bash
GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke tests/test_live_smoke_governance.py -q
```
