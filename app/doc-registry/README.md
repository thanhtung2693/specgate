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

`make run` / `make dev` run `make dev-secrets` first: it creates `.env` from `.env.example` and generates `SETTINGS_ENCRYPTION_KEY` if missing (idempotent).

To populate the local UI with realistic demo records after the service is running, use:

```bash
doc-registry --seed-demo --seed-demo-workspace-id <workspace-id>
```

In the root Docker Compose stack, run the same seed inside the service container:

```bash
docker compose exec doc-registry doc-registry --seed-demo --seed-demo-workspace-id <workspace-id>
```

The demo seed is idempotent by feature key. It creates Feature Map records, approved and draft artifacts, work items across readiness phases (`Intake`, `Draft`, `Review`, `Ready`), linked Governance Knowledge freshness warnings, tracker conflicts, tracker links, and a delivered-review verdict.

`POST /maintenance/demo-remove` is the mirror: it deletes exactly the rows and blob objects the seed creates (identified by the fixed demo IDs) and leaves all other data untouched. It is idempotent too, and returns per-category deletion counts. The `specgate demo remove` CLI command calls this endpoint on the running server.

When `STORAGE_DRIVER=s3`, the server creates the bucket on startup if
`S3_ENSURE_BUCKET=true`. Local storage is the default.

**Hot reload:** install [Air](https://github.com/air-verse/air) (`go install github.com/air-verse/air@latest`), ensure `$(go env GOPATH)/bin` is on your `PATH`, then run `make dev` (uses `.air.toml`). It rebuilds on `.go` changes. In the root Docker dev stack, `plugins/` is mounted at `watch/plugins`; Air polls that mount, syncs embedded plugin assets, and rebuilds automatically when plugin files change.

The server reads **`.env` in the current directory** (via `godotenv`) and
listens on **`:8080`** by default. **No general HTTP authentication** — keep it
inside a trusted network (see [docs/README.md](docs/README.md)). PostgreSQL is
required through `POSTGRES_DSN`. Redis is needed only with
`QUEUE_DRIVER=redis` (which drives both the inbound-webhook queue and async
Knowledge ingestion); MinIO/S3 is needed only with `STORAGE_DRIVER=s3`.

**`SETTINGS_ENCRYPTION_KEY`** (32-byte hex) is required; `make run`/`make dev` generate it into `.env` for you (set it manually only for non-Makefile runs: `openssl rand -hex 32`). Settings are shared across internal consumers and stored in the **`settings`** table; use `GET /settings` / `PUT /settings` for operator changes. Coding agents use the `specgate` CLI; see [Use SpecGate with a coding agent](../../docs/using-specgate/guides/coding-agent-workflow.md). See [docs/spec.md](docs/spec.md) section 6.

Governance Knowledge endpoints live under `/documents/*` and
`/governance/context/search`. Markdown (`.md`) and plain text (`.txt`) uploads are
parsed, chunked, embedded with the provider configured in **Settings → Models**,
and indexed in PostgreSQL with pgvector. Set `KNOWLEDGE_DRIVER=none` to disable
vector search.

Ingestion runs through the same `QUEUE_DRIVER` substrate as inbound webhooks:
create/upload returns **202** with status `uploaded` and callers poll
`GET /documents/{id}` for the terminal status in both modes. Under
`QUEUE_DRIVER=sync` (default, no Redis) ingestion runs inline within the request;
under `QUEUE_DRIVER=redis` (requires `REDIS_URL`) a worker processes the
`knowledge` queue and retries failures up to `WEBHOOK_QUEUE_MAX_RETRY`. Re-ingest
a `failed` document with `POST /documents/{id}/retry` (no delete/re-upload).

See [docs/README.md](docs/README.md) for stack details, URLs, and commands.

## Docker image

```bash
docker build -f ../docker/Dockerfile.doc-registry -t doc-registry:local ..
```

Build context is the monorepo root.
