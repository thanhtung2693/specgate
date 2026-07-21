# Configuration

Use this reference to find where a SpecGate setting lives and how it is applied.
It is organized by operator surface. Module `.env.example` files remain
canonical for exhaustive environment variable lists.

For task steps, use [Configure models](../guides/configure-models.md) or
[Operate SpecGate](../guides/operate-specgate.md).

## CLI

`specgate init` has two topologies. Interactive init defaults to **Local CLI**:
it stores state in SQLite on this machine and does not start Docker or contact a
server. Choose **Full appliance** when you need the browser, governance chat,
Knowledge, integrations, or shared server-backed workspaces. Scripted setup
must state its topology explicitly:

```bash
specgate init --mode local --no-input \
  --workspace-name "My workspace" \
  --display-name "Jane Doe" \
  --username jane
specgate init --mode full --no-input
```

Switching topology clears the other mode's server/deployment or Local-only
identity and project bindings. Create or select an identity after setup in the
new mode.

Local state defaults to the OS SpecGate config location. Use `--local-dir` at
initialization to select another store, or `SPECGATE_LOCAL_DIR` for a
non-persisting Local-command override. `SPECGATE_CONFIG_PATH` selects an
isolated CLI config file for automation and dogfood sessions.

Local mode has no Docker daemon, server, browser UI, governance chat, Knowledge,
or integrations. Its Local governed route is:

```bash
specgate artifact publish --file artifact.json
specgate gates check <artifact-id> --json --summary
specgate gates tasks list <artifact-id> --json
specgate gates tasks show <task-id> --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
specgate gates results <artifact-id> --json
specgate --yes change approve <artifact-id> --title "Implement it" \
  --ac "IDE-agent-confirmed criterion"
specgate work context <work-ref> --json
# IDE agent implements only the Context Pack, then submits evidence:
specgate change submit <work-ref> --file completion.json
specgate --yes change accept <work-ref>
specgate audit <work-ref> --json
specgate stats --json
```

Artifact documents are immutable (1 MiB per document, 10 MiB per package).
Local readiness combines deterministic checks with frozen Local IDE gate tasks;
it remains `not_run` until required semantic results are submitted and is
distinct from human approval. Full mode without a platform model uses the same
task loop and `agent_attested` trust. The authoritative unfinished-task receipt
is `dispatched_to_ide_agent.pending_task_ids`; `created_task_ids` only says what
the current dispatch created.
Human artifact promotion and delivery decisions require `--yes` in noninteractive
sessions. A delivery approval closes that Local work item; submit any revised
evidence before the human decision, or create new governed work for further
changes. `specgate audit <work-ref>` includes the approving human and note for
the work item's approved artifact and delivery decision, plus delivery review
events. A Local delivery pass requires exactly one evidence-backed `satisfied`
claim for each scaffolded `local-N` criterion ID. Local embeds the complete
Codex, Claude Code, and Cursor plugin package; for example,
`specgate plugins install --agent codex --project-local` does not contact a
plugin registry. Project-local installation writes only the selected IDE's
managed plugin files; it never changes repository instructions such as
`AGENTS.md`.
Use `--workspace <slug-or-id>` or `SPECGATE_WORKSPACE` for a one-command Local
workspace override; neither changes the stored selection.

Full-mode server precedence:

1. `--server`;
2. `SPECGATE_SERVER`;
3. repo `.specgate/config` (committed, optional — see below);
4. global user config file;
5. `http://localhost:3000/api/doc-registry`.

`plugins install` and `plugins doctor` intentionally skip the repository
server value: executable IDE packages come from `--registry`, then the user
server (`--server`, `SPECGATE_SERVER`, or global config).

```bash
specgate config server http://localhost:3000/api/doc-registry
```

CLI config stores connection preferences and the selected local user/workspace,
not general product credentials. It may also store project-scoped workspace
bindings keyed by a local Git root path. When a command runs from a bound
checkout, that workspace overrides the global workspace for attribution and
default filtering; outside that checkout, the global workspace remains the
fallback. A malformed global config fails closed before operational commands
contact a server or modify local state; repair or move that file aside and run
`specgate init` again. `specgate version` and help remain available for
diagnosis. Use `specgate workspace bind` from a checkout to bind the currently
selected workspace, or `specgate workspace bind <slug>` to bind a named
workspace directly; use `specgate workspace unbind` to remove the binding.
Effective workspace selection uses this precedence:

1. `--workspace <slug-or-id>`;
2. `SPECGATE_WORKSPACE`;
3. per-user project binding for the nearest Git root;
4. repo `.specgate/config` default workspace (committed, optional);
5. global workspace;
6. no workspace selected.

The repo default workspace is honored only when no per-user project binding
exists — a binding always outranks it. When the repo default is used, `specgate
status` and `specgate workspace current` show the source as `.specgate/config`.

Project bindings are stored under canonical Git root paths so they can be
removed cleanly when a checkout-specific selection is cleared. `specgate
workspace select <slug>` in non-interactive mode saves the global workspace;
use `workspace bind` when automation should bind a project. `specgate user
logout` clears both the global workspace and project-scoped workspace bindings.
Full appliance `specgate init` writes the deployment directory and saves the
gateway API URL inferred from `SPECGATE_PORT`, so the next CLI command targets the appliance it
just started. Supply `SPECGATE_PORT` or `SPECGATE_COMPOSE_PROJECT` before the
first `init`; the CLI records nonempty overrides in the deployment `.env` so
later `up`, `update`, `doctor`, and uninstall commands address the same
appliance. `update` makes the same repair for an existing deployment when an
override is supplied. The CLI preserves the `/api/doc-registry` base path when
building request URLs. It refreshes a derived `http://localhost:<port>` URL
when the configured port changes, while an explicit custom or remote server
remains untouched.

CLI environment variables:

| Variable | Purpose |
|---|---|
| `SPECGATE_SERVER` | Overrides the saved server URL. |
| `SPECGATE_WORKSPACE` | Overrides the project/global workspace selection for one command when `--workspace` is omitted. |
| `SPECGATE_LOCAL_DIR` | Overrides the selected Local SQLite state directory for one command session. |
| `SPECGATE_CONFIG_PATH` | Overrides the per-user CLI config path for an isolated session. |
| `SPECGATE_NO_UPDATE_CHECK` | Set to `1`, `true`, `yes`, or `on` to disable the public GitHub release freshness check. |
| `CI` | When truthy, suppresses human-facing public update checks. |

### Repo-level `.specgate/` directory

A project checkout has one `.specgate/` directory at the repo root with two
distinct layers:

- **Working directory (gitignored).** Transient CLI files live directly under
  `.specgate/`. `specgate delivery report --init` (bare, no explicit path)
  writes its scaffold to `.specgate/completion-<ref>.json` instead of the repo
  root. The whole directory is gitignored; pass `--init=<path>` to write a
  specific file elsewhere.
- **Shared config (committed, optional).** A single `.specgate/config` file
  (JSON) holds team defaults and slots into the precedence chains above,
  **between `SPECGATE_*` env and the global user config**. It supports exactly
  two keys:

  ```json
  {
    "server": "https://specgate.internal",
    "workspace": "platform"
  }
  ```

**What stays out of `.specgate/config`.** Secrets, API keys, and per-user
identity (which local user you are) are **never** read from the committed file —
they stay in environment variables, the deployment `.env`, or the global user
config. Any credential or identity keys added to `.specgate/config` are ignored.
The repo default `workspace` is honored only when you have no per-user project
binding; your binding always wins.

To commit the shared config, force-add it past the `.specgate/` ignore rule:

```bash
git add -f .specgate/config
```

## Full appliance

The CLI-managed local deployment exposes one port and persists one volume:

| Variable | Default | Purpose |
|---|---:|---|
| `SPECGATE_PORT` | `3000` | Public gateway port for the UI and APIs |
| `SPECGATE_COMPOSE_PROJECT` | `specgate` | Names the local Compose project |

If `specgate up` reports that its host port is in use, choose an unused port in
the deployment `.env` (for example `SPECGATE_PORT=3010`) and run `specgate up`
again. `up` updates the derived local review URL and saved CLI gateway to match
that port. Set both values before the first `specgate init` when you need a
non-default port or Compose project; those selections are then durable in
`.env`. The CLI names the conflicting port; it never exposes internal service
ports for this recovery.

The appliance keeps its internal services private to the container. Redis and
MinIO are not used by this local path. Source-checkout multi-container settings
belong to the [contributor setup guide](../../contributing/setup.md).

## Doc Registry

| Variable | Purpose |
|---|---|
| `APP_BASE_URL` | Web UI origin used in generated links |
| `UI_PORT` | Source-checkout Compose fallback used by Doc Registry when `APP_BASE_URL` is unset |
| `HTTP_ADDR` | Doc Registry listen address |
| `DELIVERY_SLA_DAYS` | Days before a failing delivery review appears as `delivery_stale`; default `7` |
| `POSTGRES_DSN` | PostgreSQL connection |
| `SETTINGS_ENCRYPTION_KEY` | required encryption key for stored secrets |
| `SENTRY_DSN` | optional error reporting |

HTTP access logs use the socket peer address. SpecGate does not trust or rewrite
client IPs from `X-Forwarded-*` headers; terminate and log trusted proxy client
identity at the proxy layer when needed.

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

The Web UI stores the server-side model that powers platform readiness and
delivery review. The governance chat agent's model is configured separately via
environment. Stored settings:

- server-side model provider and model (defaults to `openai` / `gpt-5.4-mini`);
- default reasoning effort;
- embedding provider and model;
- OpenAI, Google, Anthropic, and OpenRouter API keys.

Keys are encrypted at rest and masked on normal reads.

The agents environment may provide runtime fallback keys, but the UI settings
are the normal operator path.

## Governance thresholds and retention

Settings include:

- quality-gate confidence threshold;
- work-item lifecycle behavior;
- governance file retention.

An explicit human delivery approval moves a work item into the derived
`Delivered` phase but does not remove it from history. `specgate work list`
stays uncluttered (it is attention-scoped), while archived work remains
inspectable through an explicit ref. Clear completed work with
`specgate work archive <ref>`, or enable
`governance.auto_archive_on_delivery_pass` (default `false`; editable in the UI
General settings) to archive a work item automatically once its delivery review
is accepted by a human. SpecGate may then best-effort move the one linked Linear
issue to Done. A platform delivery pass is ready for human review and triggers
neither terminal effect.

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

Each selected provider resource owns its managed webhook credential. Operators
do not configure an integration-level webhook secret.

## Agents service boundary

Relevant values:

- `AGENTS_BASE_URL`;
- `DOC_REGISTRY_BASE_URL`.

Coding IDE agents use the CLI. The Agents service uses Doc Registry REST. Both
assume the trusted network described in the security guidance.

## LangGraph runtime

The v0.1 appliance deliberately uses the local in-memory runtime so it can
run without LangSmith deployment credentials. Governance-chat threads therefore
reset when the appliance restarts; governed artifacts, work, delivery evidence,
and settings remain durable in the appliance volume.

Durable/self-hosted runtime settings may include:

- `DATABASE_URI`;
- `REDIS_URI`;
- `LANGSMITH_API_KEY`;
- `LANGSMITH_PROJECT`;
- `LANGSMITH_HIDE_INPUTS` and `LANGSMITH_HIDE_OUTPUTS` (both default to
  `true`; explicitly set `false` only when trace payload content is intended);
- governance-ops chat model — `GOVERNANCE_OPS_MODEL_PROVIDER`,
  `GOVERNANCE_OPS_MODEL`, `GOVERNANCE_OPS_API_KEY`, and
  `GOVERNANCE_OPS_THINKING_LEVEL` configure the model the governance-ops chat
  agent runs on, isolated from the configured server-side model. The provider
  defaults to `openai`, the model id defaults to `gpt-5.4-mini`, and an API key
  is still required before chat can call the provider.
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

- [Doc Registry environment template](../../../app/doc-registry/.env.example)
- [Agents environment](../../../app/agents/README.md)
- [UI environment](../../../app/ui/README.md)
- [Full appliance bundle](../../../deploy/local/)

## Related

- [Operate SpecGate](../guides/operate-specgate.md)
- [Configure models](../guides/configure-models.md)
- [Trust and security](../concepts/trust-and-security.md)
