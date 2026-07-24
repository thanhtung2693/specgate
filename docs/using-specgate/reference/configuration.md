# Configuration

Use this reference to find user-facing SpecGate settings and how they are
applied. It covers the CLI, project configuration, the Full appliance, and
workspace settings—not the internal environment of a source checkout.

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
new mode. It does not delete Local SQLite state; use a portable export before
moving Local history into Full mode.

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
Before `specgate init` selects a topology, plugin install and doctor also use
that embedded package. This lets an IDE bootstrap finish on a clean machine
without a running appliance. An explicit `--registry`, `--server`, or
`SPECGATE_SERVER` still selects the remote package source.
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
bindings keyed by a local Git root path. `specgate init` creates the binding
for the Git checkout where initialization runs. When a command runs from a bound
checkout, that workspace overrides the global workspace for attribution and
default filtering. In a different Git checkout, workspace-scoped commands stop
until it is bound, given a repo default, or run with an explicit workspace
override. Outside Git checkouts, the global workspace remains available. A
malformed global config fails closed before operational commands
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

Step 5 is not accepted implicitly by workspace-scoped commands inside a Git
checkout. Run `specgate workspace bind`, commit an appropriate repo default, or
use an explicit one-command override.

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
| `SPECGATE_COMPOSE_PROJECT` | `specgate` for the default deployment; path-scoped for a new custom `--dir` | Names the local Compose project; an explicit value overrides automatic isolation |

If `specgate up` reports that its host port is in use, choose an unused port in
the deployment `.env` (for example `SPECGATE_PORT=3010`) and run `specgate up`
again. `up` updates the derived local review URL and saved CLI gateway to match
that port. Set both values before the first `specgate init` when you need a
non-default port or Compose project; those selections are then durable in
`.env`. The CLI names the conflicting port; it never exposes internal service
ports for this recovery.

The appliance keeps its implementation services private to the container. Its
data and credentials are managed by `specgate init`, `specgate update`, and the
Settings UI. Do not add database, storage, queue, or service-runtime variables
to project configuration. Those source-checkout concerns belong to the
[contributor setup guide](../../contributing/setup.md).

## Models and embeddings

The Web UI stores the server-side model that powers platform readiness and
delivery review. Settings include:

- server-side model provider and model (defaults to `openai` / `gpt-5.4-mini`);
- default reasoning effort;
- embedding provider and model for experimental Knowledge;
- OpenAI, Google, Anthropic, and OpenRouter API keys.

Keys are encrypted at rest and masked on normal reads. Governance chat is a
separate Full-appliance capability; ask the appliance operator to configure it
when it is unavailable.

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

## Integrations

Integrations are Full-mode only. Connect a provider in the browser UI, choose
the repository or work-tracking resource it may manage, and let SpecGate create
the resource-specific webhook credential. See [Connect delivery
integrations](../guides/connect-integrations.md) for the workflow.

## Related

- [Operate SpecGate](../guides/operate-specgate.md)
- [Configure models](../guides/configure-models.md)
- [Trust and security](../concepts/trust-and-security.md)
