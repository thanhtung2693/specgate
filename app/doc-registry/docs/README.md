# Doc Registry

Central artifact store for the AI-assisted development system. Stores governance artifacts (PRD, spec, execution plans, manifest) with versioning, lifecycle, conflict detection, and an event log. See [prd.md](prd.md) and [spec.md](spec.md).

## Stack

- Go 1.26.4
- Postgres — metadata, manifest, events (see spec §3.2)
- GORM — ORM over Postgres (raw-SQL migrations remain authoritative per spec §3.2)
- S3 / MinIO — artifact files via presigned URLs
- pgvector — Governance Knowledge vector index (PostgreSQL extension)
- chi — HTTP router
- **huma v2** — OpenAPI 3.1 generation + Swagger UI (code-first)
- **Sentry** (optional) — `SENTRY_DSN` enables `sentry-go` for HTTP panics + `request_id` tag; see [spec.md](spec.md) §13

**Security:** Internal-only service — no HTTP auth. The registry is reachable from the governance-ops service and other agents inside the deployment network boundary; trust comes from network isolation, not in-band tokens.

## Layout ([golang-standards/project-layout](https://github.com/golang-standards/project-layout))

All paths are relative to the **Doc Registry repository root** (`doc-registry/`):

```
cmd/doc-registry/     # HTTP entry point
internal/
  api/                # chi router + huma API (OpenAPI 3.1)
  artifact/           # domain model (GORM tags) + service interface
  knowledge/          # Governance Knowledge ingestion, chunking, versioning, retrieval
  storage/db/         # GORM repository + Postgres driver
  storage/s3/         # S3 client + presigned URLs
  storage/pgvector/   # pgvector store (knowledge chunks)
  config/             # env-based configuration
  observability/      # optional Sentry wiring
migrations/postgres/  # Postgres schema (spec §3.2) — authoritative
../docker/Dockerfile.doc-registry
                      # container image (monorepo root)
docker-compose.yml    # local MinIO + pgvector (postgres)
.env.example
docs/                 # this folder — PRD, spec
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `HTTP_ADDR` | `:8080` | HTTP listen address. |
| `APP_ENV` | `development` | Deployment environment tag surfaced in logs and Sentry events. |
| `APP_BASE_URL` | `http://localhost:3000` | SpecGate UI origin used to build the work-item permalink embedded in an outbound tracker (Linear) issue. Set to the deployed UI URL in production. |
| `AGENTS_BASE_URL` | _(empty)_ | Internal base URL of the agents service (FastAPI). When set, the MCP server registers the `specgate_check_readiness` tool, which delegates the LLM readiness compute to agents (`POST /artifacts/{id}/run-readiness`). Empty disables the tool. |
| `DATABASE_DRIVER` | `postgres` | Storage driver. Only `postgres` is supported. |
| `POSTGRES_DSN` | _(empty)_ | libpq connection URL. Example: `postgres://doc:doc@localhost:5432/doc_registry?sslmode=disable`. |
| `SETTINGS_ENCRYPTION_KEY` | _(required)_ | AES-256-GCM key, 32 bytes as hex (64 chars). Process refuses to start if missing. Generate with `openssl rand -hex 32`. |
| `S3_ENDPOINT` | _(empty)_ | S3 / MinIO endpoint URL (e.g. `http://localhost:9000`). Required for MinIO; leave blank for AWS S3 (SDK uses region). |
| `S3_REGION` | `us-east-1` | AWS / MinIO region. |
| `S3_BUCKET` | `doc-registry` | Bucket name for artifact files. |
| `S3_ACCESS_KEY` | _(empty)_ | S3 access key ID. |
| `S3_SECRET_KEY` | _(empty)_ | S3 secret access key. |
| `S3_USE_PATH_STYLE` | `true` | Enable path-style S3 addressing. Required for MinIO; set `false` for AWS. |
| `S3_ENSURE_BUCKET` | `true` | Create the bucket on startup if it does not exist. Safe to leave enabled. |
| `S3_KEY_PREFIX` | `doc-registry/` | Key prefix for all objects stored in the bucket. |
| `S3_SIGNED_URL_TTL` | `15m` | Presigned URL TTL for artifact file downloads. |
| `S3_GOVERNANCE_UPLOAD_PUT_TTL` | `15m` | Presigned PUT URL TTL for internal governance-file uploads. |
| `QUEUE_DRIVER` | `sync` | Webhook processing backend: `sync` (inline, no Redis required, default) or `redis` (async via asynq, requires `REDIS_URL`). |
| `STORAGE_DRIVER` | `local` | Blob storage backend: `local` (filesystem under `BLOB_DATA_ROOT`, default) or `s3` (MinIO/S3, requires `S3_ENDPOINT`). |
| `BLOB_DATA_ROOT` | `/data/blobs` | Root directory for local blob storage (used when `STORAGE_DRIVER=local`). |
| `REDIS_URL` | _(empty)_ | Redis connection URI (`redis://host:port/db`). Required when `QUEUE_DRIVER=redis`. For local compose: `redis://localhost:6379`. |
| `REDIS_KEY_PREFIX` | `DOC_REGISTRY:` | Key prefix for all Redis entries (asynq queues, etc.). |
| `WEBHOOK_QUEUE_CONCURRENCY` | `10` | Async webhook worker parallelism (only used when `REDIS_URL` is set). |
| `WEBHOOK_QUEUE_MAX_RETRY` | `5` | Max automatic retries per failed webhook task before it is archived (asynq). |
| `KNOWLEDGE_MAX_FILE_BYTES` | `10485760` | Max file size (bytes) accepted for Governance Knowledge uploads (default 10 MiB). |
| `KNOWLEDGE_EMBEDDING_DIM` | `1024` | Embedding vector dimension for Governance Knowledge. Must match the embedding model used. |
| `KNOWLEDGE_DRIVER` | `none` | Vector search backend (experimental, opt-in): `pgvector` (uses Postgres) or `none` (disabled, default). |
| `GOVERNANCE_UPLOAD_MAX_BYTES` | `26214400` | Max bytes accepted for internal governance-file uploads; rejected at presign (default 25 MiB). |
| `GOVERNANCE_FILES_PURGE_INTERVAL_HOURS` | `24` | Cadence of the in-process governance_files TTL ticker (float allowed). The retention window itself is the `governancefiles.ttl_days` setting (default 90), configurable via `PUT /settings` / the Settings UI — not an env var. |
| `OPENAPI_ENABLED` | `true` | Serve the Swagger UI at `/docs` and OpenAPI spec at `/openapi.json`. Set `false` to disable in production if desired. |
| `LOG_LEVEL` | `info` | Zerolog log level: `trace`, `debug`, `info`, `warn`, `error`. |
| `LOG_FORMAT` | `json` | Log output format: `json` (production) or `text` (human-readable dev output). |
| `SENTRY_DSN` | _(empty)_ | Sentry DSN. Empty = Sentry disabled. |
| `SENTRY_ENVIRONMENT` | _(APP_ENV)_ | Sentry environment tag. Defaults to `APP_ENV` when blank. |
| `SENTRY_RELEASE` | _(empty)_ | Sentry release identifier (e.g. a git SHA). |
| `SENTRY_TRACES_SAMPLE_RATE` | `0` | Sentry performance tracing sample rate (0–1). `0` = disabled. |
| `OAUTH_PUBLIC_CALLBACK_BASE_URL` | _(empty)_ | **Optional override** for the public origin OAuth providers redirect back to (callback path `/integrations/oauth-callback` is appended). Empty = derived from the request (no config for local dev); set it only behind a reverse proxy / in prod where the request host isn't the public one. |
| `GITHUB_OAUTH_CLIENT_ID` / `GITHUB_OAUTH_CLIENT_SECRET` | _(empty)_ | GitHub OAuth app credentials. Blank = GitHub OAuth not configured. |
| `GITLAB_OAUTH_CLIENT_ID` / `GITLAB_OAUTH_CLIENT_SECRET` | _(empty)_ | GitLab OAuth app credentials. Blank = GitLab OAuth not configured. |
| `LINEAR_OAUTH_CLIENT_ID` / `LINEAR_OAUTH_CLIENT_SECRET` | _(empty)_ | Linear OAuth app credentials. Blank = Linear OAuth not configured. |
| `LINEAR_WEBHOOK_SECRET` | _(empty)_ | Linear inbound-webhook secret (HMAC key for `Linear-Signature`); set the same value in Linear. Blank = inbound Linear webhooks are rejected with 401. GitLab and GitHub do not use an env secret — each integration carries a self-served per-integration secret via `/integrations/{id}/webhook-secret` (GitLab: paste the GitLab-generated `whsec_` signing token; GitHub: a Doc-Registry-generated secret to set in GitHub). |

## Local development

From the **Doc Registry root** (`doc-registry/`):

```bash
make setup          # first time only: creates .env, generates SETTINGS_ENCRYPTION_KEY
# Edit .env: set POSTGRES_DSN (and other optional vars)
make docker-up      # starts MinIO (:9000/:9001) + pgvector-postgres
make tidy           # resolve dependencies
make run            # starts server on :8080
# optional: go install github.com/air-verse/air@latest  →  make dev  (hot reload)
```

The app now creates the S3 bucket on startup when `S3_ENSURE_BUCKET=true` (the default in `.env.example`). If you disable that flag, provision the bucket out of band before writing artifacts.

| URL | Purpose |
|---|---|
| `http://localhost:8080/healthz` | Liveness |
| `http://localhost:8080/docs` | Swagger UI (huma auto-generated) |
| `http://localhost:8080/openapi.json` | OpenAPI 3.1 spec |
| `http://localhost:8080/mcp/info` | MCP status + tool catalog (see spec §6.6) |
| `http://localhost:8080/settings` | Settings (GET/PUT) |
| `http://localhost:8080/mcp/stream` | MCP streamable HTTP (Bearer `mcp.api_key`; same port as `HTTP_ADDR`) |
| `http://localhost:9001` | MinIO console |

## Docker image

```bash
docker build -f ../docker/Dockerfile.doc-registry -t doc-registry:local ..
```

## Common commands

| Command | Description |
|---|---|
| `make setup` | First-time setup: create `.env` and generate required secrets (idempotent) |
| `make build` | Build binary to `bin/doc-registry` |
| `make run` | Run server locally |
| `make dev` | Run with [Air](https://github.com/air-verse/air) hot reload (`.air.toml`) |
| `make test` | Run tests with race detector |
| `make lint` | `go vet` |
| `make fmt` | gofmt |
| `make migrate` | Apply migrations and exit |
| `make docker-up` | Start MinIO + pgvector-postgres (bucket creation is handled by the app when enabled) |
| `make docker-down` | Stop supporting services |

### `make test` and the Postgres subtests

`make test` runs every storage test against **both** drivers via `forEachDriver`
(`internal/storage/db/testutil_test.go`). The `postgres` subtest spins a real
`postgres:18-alpine` container with **testcontainers**, which talks to the Docker
daemon and auto-pulls `postgres:18-alpine` + `testcontainers/ryuk` on first use.
API integration tests use the same pattern for HTTP → repository coverage: one
package-level Postgres testcontainer is shared, and each DB-backed test gets an
isolated migrated database. On GitHub Actions, the `internal/api` package time is
mostly container startup plus repeated schema migration rather than handler CPU.

If those images aren't cached and the registry (docker.io) is throttled/offline,
the pull can stall until the Go 10-minute package timeout fires — surfacing as a
`storage/db` FAIL with a stuck `net/http` dial in the dump (the pull, not a code
bug). Two ways through:

- **Warm the cache** (the durable fix): `docker pull postgres:18-alpine` and
  `docker pull testcontainers/ryuk:0.13.0` once; cached pulls take seconds and the
  subtests pass.
- **Skip the Docker path** when you only need logic coverage: `DOCKER_HOST=tcp://127.0.0.1:1 make test` makes testcontainers fail fast so each `postgres` subtest `Skip`s. Tests use testcontainers-go (`postgres:18-alpine`); each subtest creates an isolated database via `forEachDriver`.

## Operational subcommands

The `doc-registry` binary recognises two one-shot flags that apply changes to
the same database the server is configured against, then exit. Both are
**idempotent** — safe to re-run.

| Flag | What it does |
|---|---|
| `--migrate-only` | Apply every pending SQL migration from the embedded postgres migration set, then exit. |
| `--seed-skills` | Create missing starter rubric skills. Existing skills are left untouched so teams can edit or replace them. |
| `--seed-demo` | Create local demo planning data for UI development, then exit. Idempotent — safe to re-run. |
| `--seed-demo-workspace-id <id>` | With `--seed-demo`, assign demo work items to a workspace. Re-running also backfills existing demo work items. |
| `--seed-demo-created-by <username>` | With `--seed-demo`, record the creator for demo work items. Re-running also backfills existing demo work items. |

All flags accept the single-dash form too — Go's `flag` package treats them identically.

Required env (same as normal startup):

- `DATABASE_DRIVER=postgres` + `POSTGRES_DSN`
- `SETTINGS_ENCRYPTION_KEY` — 32-byte hex AES key (`openssl rand -hex 32`)

### Local development

```bash
# Apply migrations (Makefile shortcut)
make migrate

# Or directly:
go run ./cmd/doc-registry --migrate-only
go run ./cmd/doc-registry --seed-skills
go run ./cmd/doc-registry --seed-demo
go run ./cmd/doc-registry --seed-demo --seed-demo-workspace-id ws-1 --seed-demo-created-by thanhtung2693
```

### Docker Compose

```bash
# One-shot at deploy time (fresh container):
docker compose run --rm doc-registry doc-registry --migrate-only
docker compose run --rm doc-registry doc-registry --seed-skills

# Or against an already-running container:
docker compose exec doc-registry doc-registry --migrate-only
docker compose exec doc-registry doc-registry --seed-skills
```

### Kubernetes

Inside a running pod (the binary is on `$PATH`):

```bash
kubectl exec -it <doc-registry-pod> -- doc-registry --migrate-only
kubectl exec -it <doc-registry-pod> -- doc-registry --seed-skills
```

Already shelled into the pod? Just run the binary directly:

```sh
doc-registry --migrate-only
doc-registry --seed-skills
```

For CI/CD, a one-shot Job using the same image + `envFrom` as the Deployment
is the cleanest pattern — the Job runs `command: ["doc-registry",
"--migrate-only"]`, exits, and gets cleaned up automatically.

### When to seed

Agent node configuration is seeded automatically at migrate time: the
migration inserts a default `governance.node_config` settings row if absent. The
`--seed-skills` subcommand is only needed when:

- A new build adds skills not yet present in the running database
- A new build updates the default governance skill/model/prompt/MCP overlays

Governance Knowledge API:

| Endpoint | Purpose |
|---|---|
| `POST /documents/text` | Create text knowledge and index chunks |
| `POST /documents/upload` | Store uploaded knowledge; text-like files are indexed |
| `GET /documents` | List knowledge versions |
| `POST /governance/context/search` | Retrieve chunks for Governance |

## Status

Artifact publish/read APIs, Governance Knowledge MVP APIs, workboard records,
and governance-chat file uploads are implemented. The schema is defined by the
embedded migration set in `migrations/postgres/*.migration`. During
development, `0001_init.migration` is the collapsed fresh-install schema. Dead
tables (`mcp_external_servers`, `integration_credentials`, `epics`, `cards`)
are excluded.
