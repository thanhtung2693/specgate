# Doc Registry Contributor Rules

Extends the root [contributor rules](../../AGENTS.md). This file applies only to
changes under `app/doc-registry/`.

## Stack

- Go 1.26+, Chi, Huma v2, GORM, Postgres, and S3-compatible object storage.
- Embedded raw-SQL migrations under `migrations/postgres/` are authoritative.
  Never use GORM `AutoMigrate`.
- Postgres tests use testcontainers with `postgres:18-alpine`.
- Optional Sentry integration lives under `internal/observability/`.

## Architecture boundaries

- Domain behavior lives in `internal/artifact/`, persistence in
  `internal/storage/db/`, and HTTP handlers in `internal/api/`.
- This is an internal service with a network trust boundary and no HTTP
  authentication layer. Do not add JWT/RBAC middleware without changing the
  architecture and `docs/spec.md` §7.
- Status transitions and their `artifact_events` row are one transaction.
- Object bodies go through the configured storage driver. Do not bypass the
  workspace-scoped repository or construct unscoped object keys.
- API changes use Huma operation definitions and update the route contract in
  `docs/api.md` plus the section 6 entry point in `docs/spec.md` when the
  boundary changes.

## Database and migration rules

- During pre-release development, keep the complete current schema in the
  idempotent `migrations/postgres/0001_init.migration`; do not add compatibility
  migrations or backfills for discarded development schemas.
- Test the collapsed migration against a fresh database, and do not leave
  application code depending on columns absent from it.
- Never drop or purge the database, object bucket, or MinIO data directory
  without explicit confirmation and a verified target.

## Tests and commands

```bash
make test
make lint
make build
make migrate
```

- Keep tests co-located with source unless an existing integration suite owns
  the scenario.
- Preserve existing `t.Parallel` usage.
- Use `t.Setenv` and `t.TempDir`; do not mutate shared process or filesystem
  state.
- Integration tests cover HTTP → database → object-store boundaries and use
  the repository's existing build-tag/testcontainer pattern.

## Documentation

- Endpoint or payload change: update `docs/api.md`.
- Status or event change: update the owning spec sections, domain enums, DTO
  enums, event writes, and `docs/contributing/contracts.md` when shared.
- Environment change: update config code/tests, `.env.example`, and
  `docs/README.md`.
- Storage, encryption, authentication, or trust-boundary change: update the
  architecture docs and add an ADR when the decision is durable.

Never log full credentials, JWTs, secret settings, or signed object URLs.
