# Doc Registry developer reference

This is the authoritative module guide for contributors. Doc Registry is the
SpecGate service that stores workspace-scoped governance artifacts, work items,
delivery evidence, Knowledge context, integration signals, and their audit
history. Product installation and coding-agent workflows belong in
[`docs/using-specgate/`](../../../docs/using-specgate/README.md).

Read the [PRD](prd.md), [technical specification](spec.md), and [REST API
contract](api.md) before changing the corresponding behavior.

## Stack

- Go 1.26.4
- Postgres — metadata, manifest, events (see spec §3.2)
- GORM — ORM over Postgres (raw-SQL migrations remain authoritative per spec §3.2)
- Local filesystem (default) or S3 / MinIO — artifact files via the shared object-store interface
- pgvector — Governance Knowledge vector index (PostgreSQL extension)
- chi — HTTP router
- **Huma v2** — OpenAPI 3.1 generation + Scalar API reference (code-first)
- **Sentry** (optional) — `SENTRY_DSN` enables `sentry-go` for HTTP panics + `request_id` tag; see [spec.md](spec.md) §13

**Security:** Internal-only service — no HTTP auth. The registry is reachable from the governance-ops service and other agents inside the deployment network boundary; trust comes from network isolation, not in-band tokens.

## Layout ([golang-standards/project-layout](https://github.com/golang-standards/project-layout))

All paths are relative to the Doc Registry module root (`app/doc-registry/`):

```
cmd/doc-registry/     # HTTP entry point
internal/
  api/                # chi router + huma API (OpenAPI 3.1)
  artifact/           # domain model (GORM tags) + service interface
  knowledge/          # Governance Knowledge ingestion, chunking, versioning, retrieval
  storage/db/         # GORM repository + Postgres driver
  storage/localobject/ # local filesystem object store
  storage/s3/         # S3 client + presigned URLs
  storage/pgvector/   # pgvector store (knowledge chunks)
  config/             # env-based configuration
  observability/      # optional Sentry wiring
migrations/postgres/  # Postgres schema (spec §3.2) — authoritative
../../docker/Dockerfile.doc-registry
                      # container image (monorepo root)
.env.example
docs/                 # this folder — PRD, spec
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `HTTP_ADDR` | `:8080` | HTTP listen address. |
| `APP_BASE_URL` | `http://localhost:3000` | SpecGate UI origin used to build generated links, including artifact review links and tracker permalinks. Set to the deployed UI URL in production. When unset, `UI_PORT` can derive a local `http://localhost:<port>` origin. |
| `UI_PORT` | _(empty)_ | Local release-stack UI port fallback for `APP_BASE_URL` when `APP_BASE_URL` is unset. |
| `AGENTS_BASE_URL` | _(empty)_ | Internal base URL of the agents service (FastAPI). When set, the REST API can delegate readiness checks, quality-gate runs, delivery review, and quick work creation to agents. Empty disables those agents-backed operations. |
| `DELIVERY_SLA_DAYS` | `7` | Days before the authoritative failing delivery-review gate run appears as a `delivery_stale` stale warning. Values `<= 0` warn on any authoritative failing delivery review. |
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
| `S3_GOVERNANCE_UPLOAD_PUT_TTL` | `15m` | Presigned PUT URL TTL for internal governance-file uploads. |
| `QUEUE_DRIVER` | `sync` | Webhook processing backend: `sync` (inline, no Redis required, default) or `redis` (async via asynq, requires `REDIS_URL`). |
| `STORAGE_DRIVER` | `local` | Blob storage backend: `local` (filesystem under `BLOB_DATA_ROOT`, default) or `s3` (MinIO/S3, requires `S3_ENDPOINT`). |
| `BLOB_DATA_ROOT` | `/data/blobs` | Root directory for local blob storage (used when `STORAGE_DRIVER=local`). |
| `REDIS_URL` | _(empty)_ | Redis connection URI (`redis://host:port/db`). Required when `QUEUE_DRIVER=redis`. For local compose: `redis://localhost:6379`. |
| `REDIS_KEY_PREFIX` | `DOC_REGISTRY:` | Key prefix for all Redis entries (asynq queues, etc.). |
| `WEBHOOK_QUEUE_CONCURRENCY` | `10` | Async webhook worker parallelism (only used when `REDIS_URL` is set). |
| `WEBHOOK_QUEUE_MAX_RETRY` | `5` | Max automatic retries per failed webhook task before it is archived (asynq). |
| `KNOWLEDGE_MAX_FILE_BYTES` | `10485760` | Positive max content size (bytes) accepted and read for Governance Knowledge text and file documents (default 10 MiB). |
| `KNOWLEDGE_EMBEDDING_DIM` | `1024` | Embedding vector dimension for Governance Knowledge. Must match the embedding model used. |
| `KNOWLEDGE_DRIVER` | `none` | Workspace-scoped Knowledge vector backend (alpha opt-in): `pgvector` (uses Postgres) or `none` (disabled, default). |
| `GOVERNANCE_UPLOAD_MAX_BYTES` | `26214400` | Positive max bytes accepted for internal governance-file uploads and reads; rejected at presign, and the accepted size is signed into presigned PUT requests (default 25 MiB). |
| `GOVERNANCE_FILES_PURGE_INTERVAL_HOURS` | `24` | Cadence of the in-process governance_files TTL ticker (float allowed). The retention window itself is the `governancefiles.ttl_days` setting (default 90), configurable via `PUT /settings` / the Settings UI — not an env var. |
| `ARTIFACT_RETENTION_SWEEP_ENABLED` | `false` | Opt-in artifact retention sweeper (spec §9). When `true`, deletes `superseded` artifacts past 90 days and `needs_changes` past 30 days; never deletes `approved` or `draft`, and skips artifacts referenced by a feature or change request. |
| `ARTIFACT_RETENTION_SWEEP_INTERVAL_HOURS` | `24` | Cadence of the artifact retention sweeper (float allowed). Only used when the sweeper is enabled. |
| `OPENAPI_ENABLED` | `true` | Serve the Scalar API reference at `/docs` and OpenAPI spec at `/openapi.json`. Set `false` to disable both in production if desired. |
| `SENTRY_DSN` | _(empty)_ | Sentry DSN. Empty = Sentry disabled. |
| `SENTRY_ENVIRONMENT` | `development` | Sentry environment tag. |
| `SENTRY_RELEASE` | _(empty)_ | Sentry release identifier (e.g. a git SHA). |
| `SENTRY_TRACES_SAMPLE_RATE` | `0` | Sentry performance tracing sample rate (0–1). `0` = disabled. |
| `OAUTH_PUBLIC_CALLBACK_BASE_URL` | _(empty)_ | **Optional override** for the public origin OAuth providers redirect back to (callback path `/integrations/oauth-callback` is appended). Empty = derived from the request (no config for local dev); set it only behind a reverse proxy / in prod where the request host isn't the public one. |
| `GITHUB_OAUTH_CLIENT_ID` / `GITHUB_OAUTH_CLIENT_SECRET` | _(empty)_ | GitHub OAuth app credentials. Blank = GitHub OAuth not configured. |
| `GITLAB_OAUTH_CLIENT_ID` / `GITLAB_OAUTH_CLIENT_SECRET` | _(empty)_ | GitLab.com OAuth app credentials. Blank = GitLab OAuth not configured; self-hosted GitLab keeps token-based setup and does not reuse these credentials. |
| `LINEAR_OAUTH_CLIENT_ID` / `LINEAR_OAUTH_CLIENT_SECRET` | _(empty)_ | Linear OAuth app credentials. Blank = Linear OAuth not configured. |

## Native module development

From the **Doc Registry root** (`doc-registry/`):

```bash
make setup          # first time only: creates .env, generates SETTINGS_ENCRYPTION_KEY
# Edit .env: point POSTGRES_DSN at a disposable development database
make tidy           # resolve dependencies
make run            # starts server on :8080
# optional: go install github.com/air-verse/air@latest  →  make dev  (hot reload)
```

For normal repository-level development, run the single-container appliance
from the repository root with `make setup`. Native module development is useful
for a focused Go loop and expects its PostgreSQL dependency to be managed
separately. Filesystem storage is the default; S3-compatible storage is an
optional self-host/cloud configuration.

| URL | Purpose |
|---|---|
| `http://localhost:8080/healthz` | Liveness |
| `http://localhost:8080/docs` | Scalar API reference (generated from Huma's OpenAPI document) |
| `http://localhost:8080/openapi.json` | OpenAPI 3.1 spec |
| `http://localhost:8080/settings` | Settings (GET/PUT) |

## Docker image

```bash
docker build -f ../../docker/Dockerfile.doc-registry -t doc-registry:local ../..
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

### `make test` and the Postgres subtests

`make test` runs every storage test against Postgres via `forEachDriver`
(`internal/storage/db/testutil_test.go`). The subtest spins a real
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

The `doc-registry` binary recognises one-shot flags that apply changes to
the same database the server is configured against, then exit. Both are
**idempotent** — safe to re-run.

| Flag | What it does |
|---|---|
| `--migrate-only` | Apply the embedded Postgres schema migrations, then exit. |
| `--seed-skills` | Create missing workspace-scoped rubric skills for scope, specification, acceptance, plan traceability, rollout, and delivery review. Model-judged gates use `pass`, `warn`, `fail`, `needs_human_review`, and `not_applicable`; `spec_completeness` uses per-topic `covered`/`partial`/`missing`/`not_applicable`; delivery review uses per-criterion `met`/`unmet`/`unclear`. Existing skills are left untouched so teams can edit or replace them. If no workspace exists, the command is a no-op; seed after workspace bootstrap. |
| `--seed-demo` | Create local demo governance data for UI development, then exit. Pass a workspace ID so work and Knowledge appear in scoped UI/API views. Idempotent — safe to re-run. |
| `--seed-demo-workspace-id <id>` | With `--seed-demo`, assign demo work items and Knowledge to a workspace. Re-running also backfills existing demo work items. |
| `--seed-demo-created-by <username>` | With `--seed-demo`, record the creator for demo work items. Re-running also backfills existing demo work items. |

All flags accept the single-dash form too — Go's `flag` package treats them identically.

Required env (same as normal startup):

- `POSTGRES_DSN`
- `SETTINGS_ENCRYPTION_KEY` — 32-byte hex AES key (`openssl rand -hex 32`)

### Local development

```bash
# Apply migrations (Makefile shortcut)
make migrate

# Or directly:
go run ./cmd/doc-registry --migrate-only
go run ./cmd/doc-registry --seed-skills # seeds each existing workspace
go run ./cmd/doc-registry --seed-demo --seed-demo-workspace-id ws-1 --seed-demo-created-by thanhtung2693
```

### Container deployment

From the repository root:

```bash
# Single-container contributor appliance:
make seed-skills

# Separable self-host/cloud topology:
docker compose -f docker-compose.yml exec doc-registry doc-registry --migrate-only
```

### Kubernetes

Inside a running pod (the binary is on `$PATH`):

```bash
kubectl exec -it <doc-registry-pod> -- doc-registry --migrate-only
kubectl exec -it <doc-registry-pod> -- doc-registry --seed-skills # seeds each existing workspace
```

Already shelled into the pod? Just run the binary directly:

```sh
doc-registry --migrate-only
doc-registry --seed-skills # seeds each existing workspace
```

For CI/CD, a one-shot Job using the same image + `envFrom` as the Deployment
is the cleanest pattern — the Job runs `command: ["doc-registry",
"--migrate-only"]`, exits, and gets cleaned up automatically.

### When to seed

Built-in gate-rubric Skills are installed automatically at server startup and
when a workspace is bootstrapped. Use `--seed-skills` only to reconcile an
existing database after a new build adds a missing starter Skill. Add
`--seed-skills-overwrite` only when an operator intentionally wants to replace
an edited starter Skill with the build's current default.

Governance Knowledge API:

| Endpoint | Purpose |
|---|---|
| `POST /documents/text` | Create text knowledge and index chunks |
| `POST /documents/upload` | Store uploaded knowledge; text-like files are indexed |
| `GET /documents` | List knowledge versions; `items` is the requested page and `total` is the full matching count before pagination |
| `POST /governance/context/search` | Retrieve chunks for Governance |

## Schema compatibility

The embedded `migrations/postgres/0001_init.migration` is the complete current
fresh-install schema. It deliberately does not upgrade development databases
created by discarded schema revisions. At startup, the registry checks the
mounted database and exits before serving requests when its required schema is
missing or incompatible. Create a fresh development database before retrying.
