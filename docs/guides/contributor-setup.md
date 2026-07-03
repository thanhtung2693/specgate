# Contributor setup

Use this guide when you want to modify SpecGate itself. If you only want to run
the product, use the [Quickstart](../quickstart.md).

## Prerequisites

- Go 1.26+
- Node.js 24+
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

## Start the development stack

```bash
make setup
```

`make setup`:

- checks common host-port conflicts;
- asks for queue and storage drivers;
- creates missing environment files;
- generates local secrets;
- starts the development Compose stack;
- uses the local LangGraph development runtime.

Seed representative data:

```bash
make seed
```

The seed is idempotent. It creates sample features, artifacts, work items,
readiness states, stale context, and delivery-review data.

## Understand the repository layout

| Path | Responsibility |
|---|---|
| `app/doc-registry` | Go service for artifacts, governance, evidence, integrations, REST, and MCP |
| `app/agents` | Python/LangGraph governance-ops service |
| `app/ui` | React web UI |
| `app/cli` | Go CLI |
| `plugins` | Canonical IDE skills, rules, hooks, and manifests |
| `docs` | Product docs, contracts, and maintainer references |

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
| API, status, event, policy, retention, contract | module spec or `docs/contracts.md` |
| user workflow | user guide or reference |
| architecture decision | ADR or maintainer docs |

## Related

- [Testing strategy](../testing.md)
- [Contracts](../contracts.md)
- [Maintainer internals](../internals/README.md)
