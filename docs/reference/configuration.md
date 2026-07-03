# Configuration

Use this reference to find where a SpecGate setting lives and how it is applied.
It is organized by operator surface. Module `.env.example` files remain
canonical for exhaustive environment variable lists.

For task steps, use [Configure models](../guides/configure-models.md) or
[Operate SpecGate](../guides/operate-specgate.md).

## CLI

Server precedence:

1. `--server`;
2. `SPECGATE_SERVER`;
3. user config file;
4. `http://localhost:8080`.

```bash
specgate config set server http://localhost:8080
```

CLI config stores connection preferences and the selected local user/workspace,
not general product credentials.
`specgate init` writes the deployment directory and, for local Compose stacks,
saves the Doc Registry URL inferred from `DOC_REGISTRY_PORT` so the next CLI
command targets the stack it just started.

CLI environment variables:

| Variable | Purpose |
|---|---|
| `SPECGATE_SERVER` | Overrides the saved server URL. |
| `SPECGATE_NO_UPDATE_CHECK` | Set to `1`, `true`, `yes`, or `on` to disable the public GitHub release freshness check. |
| `CI` | When truthy, suppresses human-facing public update checks. |

## Host ports

Root Compose overrides:

| Variable | Default |
|---|---:|
| `SPECGATE_COMPOSE_PROJECT` | `specgate` |
| `DOC_REGISTRY_PORT` | `8080` |
| `AGENTS_PORT` | `2024` |
| `UI_PORT` | `3000` |
| `POSTGRES_PORT` | `5432` |
| `REDIS_PORT` | `6379` |
| `MINIO_PORT` | `9000` |
| `MINIO_CONSOLE_PORT` | `9001` |

`REDIS_PORT`, `MINIO_PORT`, and `MINIO_CONSOLE_PORT` only matter when the
matching optional Compose profile is enabled. Default local deployments do not
start Redis or MinIO.

Optional bundled services:

| Profile | Starts | Use when |
|---|---|---|
| `redis` | Redis | `QUEUE_DRIVER=redis` or durable LangGraph runtime needs Redis |
| `s3` | MinIO | `STORAGE_DRIVER=s3` and no external S3 endpoint is provided |

Changing API ports may require rebuilding the Vite UI because public API URLs
are compiled into the static bundle.

## Doc Registry

| Variable | Purpose |
|---|---|
| `APP_ENV` | environment label |
| `APP_BASE_URL` | Web UI origin used in generated links |
| `HTTP_ADDR` | Doc Registry listen address |
| `POSTGRES_DSN` | PostgreSQL connection |
| `SETTINGS_ENCRYPTION_KEY` | required encryption key for stored secrets |
| `LOG_LEVEL`, `LOG_FORMAT` | logging |
| `SENTRY_DSN` | optional error reporting |

## Queue and storage

| Variable | Values | Purpose |
|---|---|---|
| `QUEUE_DRIVER` | `sync`, `redis` | inline or asynchronous webhook processing |
| `REDIS_URL` | URL | Redis connection when queue driver is Redis |
| `STORAGE_DRIVER` | `local`, `s3` | blob storage |
| `BLOB_DATA_ROOT` | path | local blob root |
| `S3_ENDPOINT` and related `S3_*` | provider values | S3/MinIO storage |
| `KNOWLEDGE_DRIVER` | `pgvector`, `none` | knowledge indexing |
| `KNOWLEDGE_EMBEDDING_DIM` | integer | vector width matching embedding model |

## Models and embeddings

The Web UI stores the server-side model that powers governance operations
(readiness gates, route classification, summaries, and delivery review). The
governance chat agent's model is configured separately via environment. Stored
settings:

- server-side model provider and model;
- default reasoning effort;
- embedding provider and model;
- OpenAI, Google, Anthropic, and OpenRouter API keys.

Keys are encrypted at rest and masked on normal reads.

The agents environment may provide runtime fallback keys, but the UI settings
are the normal operator path.

## Governance thresholds and retention

Settings include:

- quality-gate confidence threshold;
- feature-summary automation;
- work-item lifecycle behavior;
- governance file retention.

A passing delivery review does not remove a work item from the board — there is
no terminal "done" phase; the item stays in its handoff phase until it is
archived, so the board accumulates completed work over time. `specgate work list`
stays uncluttered (it is attention-scoped), but the full board grows. Clear
completed work with `specgate work archive <ref>`, or enable
`governance.auto_archive_on_delivery_pass` (default `false`; editable in the UI
General settings) to archive a work item automatically once its delivery review
passes. On a pass, any linked tracker issues (Linear / GitHub / GitLab) are also
closed automatically.

These are workspace-wide behavior where indicated by the UI. Review the
Settings consequence note before changing them.

## OAuth and integrations

Provider OAuth apps use:

- `GITHUB_OAUTH_CLIENT_ID`
- `GITHUB_OAUTH_CLIENT_SECRET`
- `GITLAB_OAUTH_CLIENT_ID`
- `GITLAB_OAUTH_CLIENT_SECRET`
- `LINEAR_OAUTH_CLIENT_ID`
- `LINEAR_OAUTH_CLIENT_SECRET`

`OAUTH_PUBLIC_CALLBACK_BASE_URL` overrides callback origin only when a reverse
proxy makes request-derived origin incorrect.

Webhook secrets are stored per integration where supported.

## MCP and governance-ops service boundary

Relevant values:

- `MCP_ENABLED`;
- `MCP_API_KEY`;
- `MCP_SERVER_URL`;
- `DOC_REGISTRY_MCP_ENABLED`;
- `AGENTS_BASE_URL`;
- `DOC_REGISTRY_BASE_URL`.

The MCP key protects the stream. General Doc Registry HTTP access still relies
on the trusted network.

Coding IDE agents use the CLI, not these MCP settings.

## Evidence actor administration

`ADMIN_SECRET` enables evidence actor API-key administration.

Actor keys authenticate evidence submissions and let SpecGate stamp producer
identity and trust. They do not provide general UI or REST authentication.

Leave `ADMIN_SECRET` empty when actor administration is not needed; admin routes
remain disabled.

## LangGraph runtime

Development can use the local in-memory runtime.

Durable/self-hosted runtime settings may include:

- `DATABASE_URI`;
- `REDIS_URI`;
- `LANGSMITH_API_KEY`;
- `LANGSMITH_PROJECT`;
- governance-ops chat model — `GOVERNANCE_OPS_MODEL_PROVIDER`,
  `GOVERNANCE_OPS_MODEL`, `GOVERNANCE_OPS_API_KEY`, and
  `GOVERNANCE_OPS_THINKING_LEVEL` configure the model the governance-ops chat
  agent runs on, isolated from the configured server-side model. The model
  defaults to a small hosted model.
  Server-side workloads other than chat — including the readiness quality gates —
  use the configured server-side model, not this one.

Requirements depend on the selected LangGraph deployment mode and image.

## Vite UI

Common build-time variables:

- `VITE_DOC_REGISTRY_URL`;
- `VITE_LANGGRAPH_API_URL`.

Vite variables are compiled into the static bundle. Runtime container
environment changes do not rewrite an already-built UI. The release UI image
uses same-origin defaults (`/api/doc-registry` and `/api/agents`) and the nginx
runtime proxies those paths to the Doc Registry and agents services inside the
Compose network.

## Exhaustive module references

- [Doc Registry environment template](../../app/doc-registry/.env.example)
- [Agents environment](../../app/agents/README.md)
- [UI environment](../../app/ui/README.md)
- [Release Compose examples](../../deploy/compose/)

## Related

- [Operate SpecGate](../guides/operate-specgate.md)
- [Configure models](../guides/configure-models.md)
- [Trust and security](../concepts/trust-and-security.md)
