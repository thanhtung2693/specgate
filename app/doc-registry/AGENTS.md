# doc-registry — Agent Rules

Extends root [../AGENTS.md](../AGENTS.md). Read that first; this file only adds module-specific conventions.

## Stack

- Go 1.26+ (single `go.mod` — this module is self-contained).
- Storage: **Postgres** (GORM + embedded raw-SQL migrations in `migrations/postgres/`). **Migrations are authoritative — do not use GORM AutoMigrate.** Tests use testcontainers-go (`postgres:18-alpine`).
- HTTP: `chi` + `huma v2` (OpenAPI 3.1 + Swagger UI auto-generated from operation definitions).
- Object storage: `aws-sdk-go-v2` (S3 / MinIO with presigned URLs).
- Logging: `zerolog`.
- Error monitoring: optional **Sentry** (`SENTRY_DSN`) — see `internal/observability/` and `docs/spec.md` §13.

## Architecture rules

- Package layout follows `golang-standards/project-layout`: `cmd/`, `internal/...`, `migrations/`.
- Domain model in `internal/artifact/`. Repository in `internal/storage/db/`. HTTP handlers in `internal/api/`.
- **No HTTP authentication.** Doc Registry is an internal service — trust comes from network boundary. Do not add JWT / RBAC middleware. See `docs/spec.md` §7.
- Status transitions + `artifact_events` row must be written in the **same transaction** (`docs/spec.md` §14).
- Spec section references in code comments use the form `// per spec §14`. Keep this pattern.

## Testing

- Run: `make test` (equivalent to `go test -race -count=1 ./...`).
- Tests are co-located with source files (standard Go layout). Do not move them to a separate directory.
- Keep `t.Parallel()` on tests where it already exists.
- Use `t.Setenv()` / `t.TempDir()` for isolation — do not mutate global state across tests.
- Unit tests first; integration tests (HTTP → DB → S3) go in `test/` or flagged with `//go:build integration` — choose consistent with existing tests when added.

## Commands (from `Makefile`)

| Command | Purpose |
| --- | --- |
| `make build` | Compile to `bin/doc-registry` |
| `make run` | `go run ./cmd/doc-registry` |
| `make dev` | Hot reload via Air |
| `make test` | Tests with race detector, no caching |
| `make lint` | `go vet ./...` |
| `make fmt` | `gofmt -s -w .` |
| `make tidy` | `go mod tidy` |
| `make migrate` | Apply migrations and exit |
| `make docker-up` | Start optional MinIO when using S3 storage |
| `make docker-down` | Stop supporting services |

## When adding / changing

- **New endpoint** → update `docs/spec.md` §6 (endpoint table + request/response body) and register via `huma.Register` in `internal/api/router.go`.
- **New status / event type** → update `docs/spec.md` §4 / §8 + corresponding enum in `internal/artifact/model.go` and event string literals in `internal/artifact/service_impl.go` + add to DTO enums in `internal/api/schemas_artifacts.go`.
- **New env var** → add to `internal/config/config.go`, `config_test.go`, `.env.example`, and `docs/README.md` env table.
- **New migration** → add `migrations/postgres/NNNN_<name>.migration`, idempotent (`CREATE IF NOT EXISTS`). Use `.migration` extension (not `.sql`). Test via `make migrate`.

## Safety

- Never drop the `POSTGRES_DSN`-targeted database without explicit user confirmation (contains production state).
- `data/.minio-data/` is gitignored dev fixtures — same rule.
- Do not log full JWT / credential / S3 signed URL content.
