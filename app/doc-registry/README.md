# Doc Registry

Go service: central artifact store for the AI-assisted SDLC. This directory is the **complete Doc Registry codebase** (module `github.com/specgate/doc-registry`) and can live as its own Git repository or as `doc-registry/` inside the `specgate` monorepo.

## Contents (self-contained)

| Path | Purpose |
|------|---------|
| `cmd/doc-registry` | HTTP server entrypoint |
| `internal/` | Application packages |
| `migrations/postgres/` | Postgres schema (embedded migrations) |
| `docs/` | PRD, technical spec, developer notes |
| `../docker/Dockerfile.doc-registry` | Production image build (monorepo root) |
| `docker-compose.yml` | Optional local services |
| `.env.example` | Environment template |

## Quick start

From this directory (`doc-registry/`):

```bash
make tidy
make run                      # auto-generates dev secrets, then starts the server
```

If you set `STORAGE_DRIVER=s3`, run `make docker-up` first to start local MinIO.

`make run` / `make dev` run `make dev-secrets` first: it creates `.env` from `.env.example` and generates `SETTINGS_ENCRYPTION_KEY` if missing (idempotent). The MCP access token (`mcp.api_key`) auto-generates into the settings DB on first start — no manual setup. View/copy/rotate it in **Settings → MCP** (or `GET /mcp/api-key`).

To populate the local UI with realistic demo records after the service is running, use:

```bash
doc-registry --seed-demo
```

In the root Docker Compose stack, run the same seed inside the service container:

```bash
docker compose exec doc-registry doc-registry --seed-demo
```

The demo seed is idempotent by feature key. It creates Feature Map records, approved and draft artifacts, work items across all readiness phases (`Intake`, `Draft`, `Review`, `Ready`, `Handoff`), linked Governance Knowledge freshness warnings, context-pack staleness, tracker conflicts, tracker links, and a delivered-review verdict.

When `STORAGE_DRIVER=s3`, the server creates the bucket on startup if
`S3_ENSURE_BUCKET=true`. Local storage is the default.

**Hot reload:** install [Air](https://github.com/air-verse/air) (`go install github.com/air-verse/air@latest`), ensure `$(go env GOPATH)/bin` is on your `PATH`, then run `make dev` (uses `.air.toml`). It rebuilds on `.go` changes. In the root Docker dev stack, `plugins/` is mounted at `watch/plugins`; Air polls that mount, syncs embedded plugin assets, and rebuilds automatically when plugin files change.

The server reads **`.env` in the current directory** (via `godotenv`) and
listens on **`:8080`** by default. **No general HTTP authentication** — keep it
inside a trusted network (see [docs/README.md](docs/README.md)). PostgreSQL is
required through `POSTGRES_DSN`. Redis is needed only with
`QUEUE_DRIVER=redis`; MinIO/S3 is needed only with `STORAGE_DRIVER=s3`.

**`SETTINGS_ENCRYPTION_KEY`** (32-byte hex) is required; `make run`/`make dev` generate it into `.env` for you (set it manually only for non-Makefile runs: `openssl rand -hex 32`). **MCP settings are shared** across all consumers (agents, UI, automation): stored in the **`settings`** table; use `GET /settings` / `PUT /settings` or the UI. Streamable MCP is served on the **same** HTTP port as the REST API at **`/mcp/stream`**, gated by `Bearer mcp.api_key` (auto-generated; connect governance-ops agents from **Settings → MCP**). Coding agents use the `specgate` CLI instead of connecting to MCP directly — see [`docs/agent-handoff.md`](../docs/agent-handoff.md). See [docs/spec.md](docs/spec.md) sections 6.5–6.6.

Governance Knowledge endpoints live under `/documents/*` and
`/governance/context/search`. Markdown (`.md`) and plain text (`.txt`) uploads are
parsed, chunked, embedded with the provider configured in **Settings → Model**,
and indexed in PostgreSQL with pgvector. Set `KNOWLEDGE_DRIVER=none` to disable
vector search.

See [docs/README.md](docs/README.md) for stack details, URLs, and commands.

## Docker image

```bash
docker build -f ../docker/Dockerfile.doc-registry -t doc-registry:local ..
```

Build context is the monorepo root.
