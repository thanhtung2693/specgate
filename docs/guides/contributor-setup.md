# Contributor setup

Use this path when modifying SpecGate itself. If you only want to try the
product, use the [CLI quickstart](../quickstart.md).

## Prerequisites

- Go 1.26+
- Node.js 24+
- Python 3.12+
- [`uv`](https://docs.astral.sh/uv/)
- Docker with Docker Compose v2

## Clone and start the development stack

```bash
git clone https://github.com/thanhtung2693/specgate.git
cd specgate
make setup
```

`make setup`:

- checks common host ports;
- asks for queue and storage drivers;
- creates missing module environment files;
- generates local secrets;
- starts the development Compose stack;
- uses the keyless local LangGraph development runtime.

Configure the model later — see [Configure models](configure-models.md).

## Seed demo data

```bash
make seed
```

The seed is idempotent and creates representative features, artifacts, work
items, readiness states, stale context, and delivery-review data.

## Repository layout

| Path | Module |
|---|---|
| `app/doc-registry` | Go artifact, governance, evidence, integration, REST, and MCP service |
| `app/agents` | Python/LangGraph governance-ops service |
| `app/ui` | React Web UI |
| `app/cli` | Go CLI |
| `plugins` | Canonical IDE Skills, rules, hooks, and manifests |
| `docs` | Product docs and cross-module contracts |

## Work on one module

Read root [AGENTS.md](../../AGENTS.md) and the module’s nested `AGENTS.md`
before editing.

### Doc Registry

```bash
cd app/doc-registry
make test
```

### Governance-ops

```bash
cd app/agents
uv sync --all-groups
uv run pytest
```

### Web UI

```bash
cd app/ui
npm install
npm run test -- --run
```

### CLI

```bash
cd app/cli
make test
```

Use the narrowest relevant verification while iterating. Run the full module
suite when a change affects shared state, routing, contracts, or build setup.

## Refresh dependencies

Use the module package manager and commit the lockfile or module checksum
changes with the manifest change:

```bash
cd app/doc-registry && go get -u -t ./... && go mod tidy
cd app/agents && uv lock --upgrade && uv sync --all-groups
cd app/ui && npm install <package>@latest && npm install -D <dev-package>@latest
```

After dependency refreshes, run the touched module's test, lint, and build
commands before opening a PR.

## Plugin changes

Canonical files live under `plugins/skills/` and `plugins/hooks/`.

After editing:

```bash
make sync-plugins
make check-plugins
```

Commit synchronized copies with the canonical source.

## Spec-driven workflow

Behavior changes require the relevant spec or contract and user documentation
in the same change.

Source-of-truth hierarchy:

1. PRD for product intent;
2. module specs and `docs/contracts.md` for behavior;
3. user docs for supported workflows;
4. code and tests for implementation and proof;
5. design specs and plans for decision history.

## Read the contributor contracts

- [Contributing](../../CONTRIBUTING.md)
- [Agent rules](../../AGENTS.md)
- [Maintainer internals](../internals/README.md)
- [Testing strategy](../testing.md)
- [Cross-module contracts](../contracts.md)
