# Contributor setup

Use this guide when you want to modify SpecGate itself. If you only want to run
the product, use the [Quickstart](../using-specgate/quickstart.md).

## Prerequisites

- Go 1.26+
- Node.js 26+ (matches CI)
- Python 3.12+
- `uv`
- Docker with Docker Compose v2

## Clone the repository

```bash
git clone https://github.com/thanhtung2693/specgate.git
cd specgate
```

Read the repository rules before editing:

- [Agent rules](../../AGENTS.md)
- [Contributing](../../CONTRIBUTING.md)

## Start the source integration stack

```bash
make setup
```

`make setup` starts the multi-container topology used to develop and validate
self-hosted/cloud deployments. It is for contributors; product users should use
the single-container appliance from the [Quickstart](../using-specgate/quickstart.md).

It:

- checks common host-port conflicts;
- creates missing environment files;
- generates local secrets;
- starts the source integration Compose stack;
- uses the local LangGraph development runtime.

The source stack defaults to synchronous queues and local file storage, so
setup does not ask infrastructure questions most contributors do not need.
Advanced Redis and S3/MinIO overrides remain available in the root
`.env.example` and `app/doc-registry/.env.example`, respectively.

Optional source-stack services use Compose profiles:

| Profile | Starts | Use when |
|---|---|---|
| `redis` | Redis | `QUEUE_DRIVER=redis` or the durable LangGraph runtime needs Redis |
| `s3` | MinIO | `STORAGE_DRIVER=s3` and no external S3 endpoint is configured |

Changing source-stack API ports may require rebuilding the Vite UI because its
public API URLs are compiled into the static bundle.

Seed representative data:

```bash
specgate workspace list
make seed DEMO_WORKSPACE_ID=<workspace-id>
```

The seed is idempotent. It creates the fixed demo features, artifacts, work
items, readiness states, stale context, and delivery-review data in the chosen
workspace.

To smoke-test the appliance from the current checkout, build its `dev` image
and initialize the checked-in local bundle:

```bash
docker build -f docker/Dockerfile.local -t ghcr.io/thanhtung2693/specgate:dev .
specgate init --mode full --dir deploy/local --bundle-version dev --no-seed
```

This is a contributor-only source build: it embeds PostgreSQL/pgvector, the Go
registry, the Python/LangGraph runtime, and the UI, so the first build is
substantially larger and slower than a normal install. The Dockerfile caches
Go, npm, and uv dependency layers; after that first build, source-only edits
reuse those layers. Normal released installs pull the prebuilt, multi-arch
appliance image selected by the CLI; they do not compile the bundle locally.

## Understand the repository layout

| Path | Responsibility |
|---|---|
| `app/doc-registry` | Go service for artifacts, governance, evidence, integrations, and REST |
| `app/agents` | Python/LangGraph governance-ops service |
| `app/ui` | React web UI |
| `app/cli` | Go CLI |
| `plugins` | Canonical IDE skills, rules, hooks, and manifests |
| `docs/using-specgate` | Product tutorials, guides, concepts, and references |
| `docs/contributing` | Durable contributor guidance: architecture, contracts, testing, accepted decisions, and release process; never per-task plans or design drafts |

Each module may have its own `AGENTS.md`. Read it before editing that module.

## Work on a module

Doc Registry:

```bash
cd app/doc-registry
make test
```

Governance-ops:

```bash
cd app/agents
uv sync --all-groups
uv run pytest
```

Web UI:

```bash
cd app/ui
npm install
npm run test -- --run
```

CLI:

```bash
cd app/cli
make test
```

Use narrow tests while iterating. Run a full module suite when a change touches
shared state, contracts, routing, build setup, or user-facing workflow.

## Update plugins

Canonical plugin assets live under `plugins/`.

After editing skills, hooks, rules, or manifests:

```bash
make sync-plugins
make check-plugins
```

Commit synchronized generated copies with the canonical source.

## Refresh dependencies

Use the module package manager and commit lockfile or checksum updates with the
manifest change:

```bash
cd app/doc-registry && go get -u -t ./... && go mod tidy
cd app/agents && uv lock --upgrade && uv sync --all-groups
cd app/ui && npm install <package>@latest
```

Run the touched module’s tests after dependency changes.

## Follow the spec-driven workflow

Behavior changes need matching docs or specs in the same change.

Use the narrowest document layer:

| Change | Update |
|---|---|
| product intent | PRD |
| API, status, event, policy, retention, contract | module spec or `docs/contributing/contracts.md` |
| user workflow | user guide or reference |
| architecture decision | ADR or `docs/contributing/architecture.md` |

## Related

- [Testing strategy](testing.md)
- [Contracts](contracts.md)
- [Architecture](architecture.md)
- [Release guide](release.md)
