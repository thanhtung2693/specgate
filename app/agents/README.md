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
  via the LangGraph CLI / server, as below. The chat agent is an experimental,
  in-development feature; the chat-surface endpoint (thread title) is inert in
  the FastAPI-only mode.

### Fast loop (in-memory checkpoints, with chat)

```bash
uv run langgraph dev
```

Default API: `http://127.0.0.1:2024` (see LangGraph CLI docs). Checkpoints and
thread metadata live in memory and disappear when the process stops. Long dev
sessions with large agent state (markdown fields, message history) can push
memory high — restart the server or prune threads when that happens.

### Postgres persistence (recommended for long sessions)

`langgraph dev` does **not** use Postgres. To store checkpoints and threads in
the shared Postgres container instead:

1. Start deps (Postgres at minimum; Doc Registry if publishing; Redis only for
   durable LangGraph / queue drivers that require it; MinIO only for S3 storage):

   ```bash
   docker compose up -d postgres doc-registry
   docker compose --profile redis up -d redis   # optional
   docker compose --profile s3 up -d minio      # optional
   ```

2. Ensure the `langgraph` database exists (fresh volumes get it from
   `docker/postgres-init/`; existing volumes: `docker exec <pg> createdb -U docreg langgraph`).

3. Set `LANGSMITH_API_KEY` in `app/agents/.env` (required by the self-hosted API image).

4. Run the Postgres-backed API (Docker) on the same port the UI expects:

   ```bash
   uv run langgraph up --port 2024 \
     --postgres-uri postgres://docreg:docreg@host.docker.internal:5432/langgraph?sslmode=disable
   ```

   `langgraph.json` loads `app/agents/.env`. Use `host.docker.internal` (not
   `127.0.0.1`) for `DATABASE_URI`, `REDIS_URI`, and Doc Registry URLs so the
   API container can reach services on the host. On macOS/OrbStack those hostnames
   also work from the host shell.

   Or use the compose `agents` service (`docker compose up agents`), which sets
   `DATABASE_URI` / `REDIS_URI` and maps `2024:8000`.

   For live iteration on the compose service, pair it with `docker compose
   watch agents` — the service declares a `develop.watch` block that syncs
   `agents/src/` into `/deps/agents/src/`. The LangGraph process imports the
   graph once at startup, so synced `.py` edits are not live until you restart
   the container (`docker restart specgate-agents-1`). Edits to
   `pyproject.toml` / `uv.lock` trigger a full rebuild. Without `compose
   watch`, a bare restart still uses the baked image source; rebuild/recreate or
   use `langgraph up --watch` / native `langgraph dev` for that mode. See
   [`/docs/contributing/testing.md` §Docker reload rule for agents](../../docs/contributing/testing.md#docker-reload-rule-for-agents).

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
