# SpecGate — Deployment Guide

For user-facing operations, see
[Operate SpecGate](../docs/guides/operate-specgate.md). This page documents
the release Compose bundle and manual deployment details.

Self-host SpecGate using pre-built GHCR images and Docker Compose.
No source checkout or build toolchain required.

## Prerequisites

- Docker + Docker Compose v2
- Optional: a [LangSmith](https://smith.langchain.com) API key when you want
  governance-ops tracing.
- Optional: an LLM provider key (Anthropic, OpenAI, or compatible) when you want
  SpecGate's server-side gates to call a platform model. Without one, IDE-agent
  workflows can still drive implementation and report delivery evidence.

## Option A — CLI quickstart (recommended)

Install the SpecGate CLI, then let it manage the stack:

```bash
# Install the CLI from GitHub
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh

# Initialize a local stack (~/.specgate by default)
specgate init

# Check what's running
specgate local-status

# Stop the stack
specgate down

# Start it again
specgate up
```

`specgate init` downloads the Compose bundle, generates a random encryption key,
copies the `.env.example` files, and starts the stack with `docker compose up --wait`.

Use `--dir <path>` to choose a non-default directory:

```bash
specgate init --dir /opt/specgate
```

Use `--seed` to load the demo dataset on first boot:

```bash
specgate init --seed
```

## Option B — Manual Compose

### 1. Get the Compose bundle

Download the files for a specific release:

```bash
VERSION=v1.0.0   # replace with the version you want
mkdir -p specgate && cd specgate

curl -fsSL "https://github.com/thanhtung2693/specgate/releases/download/${VERSION}/compose.yml" -o compose.yml
curl -fsSL "https://github.com/thanhtung2693/specgate/releases/download/${VERSION}/doc-registry.env.example" -o doc-registry.env.example
curl -fsSL "https://github.com/thanhtung2693/specgate/releases/download/${VERSION}/agents.env.example" -o agents.env.example
```

Or clone just the deploy directory from the repo:

```bash
git clone --depth 1 https://github.com/thanhtung2693/specgate.git
cp -r specgate/deploy/compose specgate-deploy && cd specgate-deploy
```

### 2. Configure environment

```bash
cp doc-registry.env.example doc-registry.env
cp agents.env.example       agents.env
```

Edit `doc-registry.env` — the only required value is a 32-byte hex key:

```bash
# Generate a random key (run once; keep the result in doc-registry.env)
openssl rand -hex 32
```

```dotenv
# doc-registry.env
SETTINGS_ENCRYPTION_KEY=<paste output here>
```

Edit `agents.env` when you want LangSmith tracing or the governance chat panel:

```dotenv
# agents.env
LANGSMITH_API_KEY=ls__...
GOVERNANCE_OPS_API_KEY=sk-...
```

Leave `LANGSMITH_API_KEY` blank for local alpha evaluation.
`GOVERNANCE_OPS_API_KEY` powers the web UI's governance chat panel (explain
gate failures, blockers, artifact context); without it the panel shows setup
instructions and stays advisory-only, while every other workflow keeps
working. Override the chat model with `GOVERNANCE_OPS_MODEL` /
`GOVERNANCE_OPS_MODEL_PROVIDER` if you don't want the default.

Configure model provider keys after startup with `specgate model set`; they are
stored encrypted in Doc Registry settings. Those settings power the
server-side ops model (gates, route classification, delivery review) — the
chat key above is separate.

### 3. Pin the version

```bash
echo "SPECGATE_VERSION=${VERSION}" > .env
```

Or use `latest` to always pull the most recent image (not recommended for production):

```bash
echo "SPECGATE_VERSION=latest" > .env
```

### 4. Start the stack

```bash
docker compose up -d --wait
```

Docker Compose will pull the images and wait until all health checks pass.

## Service URLs

| Service | URL | Notes |
|---|---|---|
| Web UI | http://localhost:3000 | Browser UI for review, settings, governance chat, and workflow scanning |
| Doc Registry | http://localhost:8080 | REST API + [Swagger](http://localhost:8080/docs) |
| Agents | http://localhost:2024 | governance-ops API |

Override host ports or the Compose project name in `.env` before startup when
you need side-by-side local stacks:

```dotenv
SPECGATE_COMPOSE_PROJECT=specgate-scratch
DOC_REGISTRY_PORT=18080
AGENTS_PORT=12024
POSTGRES_PORT=15432
```

## Connect an IDE agent

Once the stack is running, install the SpecGate CLI first:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

This installs the CLI binary. By default the installer prefers the current `specgate`
binary directory when writable, then `~/.local/bin`, then `/usr/local/bin`. After
install, point it at your local stack:

```bash
specgate config set server http://localhost:8080
specgate doctor
```

Then write IDE-specific skills, hooks, and rules:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh
```

For upgrades later, run:

```bash
specgate update
```

This refreshes the CLI from GitHub, installs IDE setup for Codex, Claude Code,
and Cursor from the public plugin registry, then updates the local Compose
bundle and images when this is a CLI-managed deployment. Human mode prints
numbered steps. Automation can consume compact progress events with
`specgate --json --json-progress update`.

## Upgrading

```bash
# Update CLI, IDE plugins, compose bundle, and images
specgate update
```

Or manually:

```bash
SPECGATE_VERSION=v1.1.0
echo "SPECGATE_VERSION=${SPECGATE_VERSION}" > .env
docker compose pull
docker compose up -d --wait
```

## Environment reference

### doc-registry.env

| Variable | Required | Description |
|---|---|---|
| `SETTINGS_ENCRYPTION_KEY` | **Yes** | 32-byte hex key for encrypting secrets at rest. Generate with `openssl rand -hex 32`. |
| `MCP_API_KEY` | No | Overrides the MCP token used by the internal governance-ops agent boundary. Coding IDE agents use the CLI. |
| `SENTRY_DSN` | No | Sentry error monitoring DSN. |

### agents.env

| Variable | Required | Description |
|---|---|---|
| `LANGSMITH_API_KEY` | **Yes** | Required by LangGraph Self-Hosted Lite for tracing. |

## Data persistence

All persistent data lives in named Docker volumes (`postgres-data`,
`doc-registry-data`). They survive container restarts
and `docker compose down`. To wipe all data:

```bash
docker compose down -v
```

## Troubleshooting

**Stack won't start** — check container logs:

```bash
docker compose logs doc-registry
docker compose logs agents
```

**`specgate doctor` fails** — confirm the stack is healthy:

```bash
specgate local-status        # shows service health
docker compose ps            # raw Compose status
```

**Encryption key error on startup** — `SETTINGS_ENCRYPTION_KEY` is empty or invalid.
Generate a fresh one with `openssl rand -hex 32` and set it in `doc-registry.env`.
