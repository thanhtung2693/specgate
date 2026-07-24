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

## Start the local appliance

```bash
make setup
```

`make setup` builds the current checkout into the single-container appliance
and starts it with `deploy/local/compose.yml`. This is the only Compose path for
local development and matches the Full-mode topology users install. Its
`specgate-dev` Compose project keeps contributor containers and data separate
from an appliance installed through the released CLI.

It:

- chooses one free gateway port;
- creates mode-`0600` appliance environment files and an encryption key;
- builds `docker/Dockerfile.local` as the `dev` appliance image;
- starts PostgreSQL, Doc Registry, Agents, the UI, and the gateway in one
  container with one named data volume.

After source changes, rebuild and recreate the appliance:

```bash
make build
make up
```

Use `make logs` for combined service logs and `make down` to stop without
deleting the named data volume.

Seed representative data:

```bash
specgate workspace list
make seed DEMO_WORKSPACE_ID=<workspace-id>
```

The seed is idempotent. It creates the fixed demo features, artifacts, work
items, readiness states, stale context, and delivery-review data in the chosen
workspace.

This is a contributor-only source build: it embeds PostgreSQL/pgvector, the Go
registry, the Python/LangGraph runtime, and the UI, so the first build is
substantially larger and slower than a normal install. The Dockerfile caches
Go, npm, and uv dependency layers; after that first build, source-only edits
reuse those layers. Normal released installs pull the prebuilt, multi-arch
appliance image selected by the CLI; they do not compile the bundle locally.

The root `docker-compose.yml` remains the separable self-host/cloud deployment
topology. It is not layered into local development.

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
