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
- This is an internal service behind a network trust boundary. The appliance
  gateway authenticates callers when members hold credentials and forwards the
  verified identity; this service verifies one Basic credential at
  `/internal/auth` and reads the forwarded identity where it records an actor.
  That single endpoint is the whole authentication surface — do not add JWT/RBAC
  middleware, sessions, or roles without changing the architecture,
  `docs/spec.md` §7, and
  [the gateway identity ADR](../../docs/contributing/adr/2026-07-29-gateway-asserted-identity.md).
- A new endpoint that records who decided something must read the authenticated
  identity: embed `AuthenticatedActorHeader` and resolve through `resolveActor`.
  A reflection test fails when one forgets, because the failure is otherwise
  silent — the endpoint quietly accepts a self-declared name.
- Status transitions and their `artifact_events` row are one transaction.
- Object bodies go through the configured storage driver. Do not bypass the
  workspace-scoped repository or construct unscoped object keys.
- API changes use Huma operation definitions and update the route contract in
  `docs/api.md` plus the section 6 entry point in `docs/spec.md` when the
  boundary changes.

## Database and migration rules

- Treat released migration files as immutable. Keep
  `migrations/postgres/0001_init.migration` as the initial release baseline and
  add a numbered, idempotent migration for every later schema change.
- Test the complete migration set against both a fresh database and the prior
  released schema affected by the change. Do not leave application code
  depending on columns absent after migration.
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
