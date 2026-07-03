# Quickstart

By the end of this guide, SpecGate will be running locally, your current user
and workspace will be selected, and your coding IDE will know how to use the
`specgate` CLI. Configuring a model is optional — the deterministic governance
core runs without one, and your coding agent can handle semantic gate work.

## What you need

- macOS, Linux, or Windows (run the steps below inside WSL2)
- Docker with Docker Compose v2 — Docker Desktop on macOS/Windows, Docker Engine on Linux
- network access to download the CLI and container images

You do not need a source checkout, Go, Node.js, or Python. A model API key is
optional to get started — see [step 4](#4-configure-a-model-optional).

> **Windows:** the install script and the `specgate` CLI run in a WSL2 shell. The
> CLI also ships a native Windows binary on the [releases page](https://github.com/thanhtung2693/specgate/releases)
> if you prefer not to use WSL.

## 1. Install the CLI

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

Confirm that your shell can find it:

```bash
specgate --help
```

If the command is not found, add `~/.local/bin` to your `PATH` or use the
install location printed by the installer.

To remove the user-local setup later:

```bash
specgate uninstall
```

That stops the local stack when present, removes CLI config and SpecGate IDE
plugin files, and keeps deployment data. In an interactive terminal it shows a
checkbox list so you can keep IDE setup or select local data removal. To delete
local data non-interactively, back it up first, then run
`specgate uninstall --purge-data --yes`.

## 2. Start SpecGate

```bash
specgate init
```

The interactive setup:

- chooses a deployment directory (`~/.specgate` by default);
- downloads the Compose bundle;
- creates required environment files and secrets;
- starts the services;
- asks for your workspace, display name, username, and optional email for local
  attribution;
- asks whether you want to seed demo data and install IDE plugins.

When you seed from interactive `specgate init`, the demo work items are attached
to the workspace and username you just selected, so `specgate work list` shows
them immediately.

For an explicit, non-interactive run:

```bash
specgate init --seed --no-input    # with demo data
specgate init --no-seed --no-input # empty workspace
```

Interactive setup shows a checkbox list for Codex, Claude Code, and Cursor when
installing IDE plugins. Use `--install-plugins --plugin-agent all` with
`--no-input` when automation should install IDE plugins as part of setup.

## 3. Verify it is running

```bash
specgate local-status
specgate doctor
```

`local-status` shows each service's health. `doctor` checks CLI/server
compatibility and capability health. Default local endpoints:

| Service | URL |
|---|---|
| Web UI | `http://localhost:3000` |
| Doc Registry and Swagger | `http://localhost:8080` |
| Governance-ops | `http://localhost:2024` |

If you preconfigure custom ports in the deployment `.env` before `specgate init`,
the CLI saves the matching Doc Registry URL and `local-status` reports the
running service bindings.

> **Alpha UI note:** the web UI is available in the release stack for review,
> artifact inspection, settings, governance chat, and workflow scanning. The
> supported authoring and coding-agent handoff path remains CLI-first.

## 4. Configure a model (optional)

The deterministic governance core — policy resolution, version snapshots,
evidence validation, and trust stamping — runs with **no model configured**.

A model adds the assistive layer: route suggestion, acceptance-criteria
drafting, delivery-review judgment, and summaries. Configure it from the CLI —
no UI required:

```bash
specgate model set --provider openai --api-key <your-key>
```

Run `specgate model set` with no flags for the guided setup: choose a provider,
enter a model id, and paste the API key into a masked prompt. For OpenRouter,
the terminal opens a searchable picker from OpenRouter's public model catalog
and filters it to text-output models; choose **Manual entry** if you want to
paste an exact model id.

The server-side model powers every server-side workload, including the LLM
readiness quality gates. To choose a different provider or model, see
[Configure models](guides/configure-models.md).

**Prefer not to configure a model at all?** Your coding agent can drive scoping,
acceptance-criteria drafting, and completion reporting through SpecGate skills —
see [Use SpecGate with a coding agent](guides/coding-agent-workflow.md). The
human stays in the loop; the agent does the drafting.

## 5. Connect your IDE

Install the SpecGate integration for your coding agent:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh -s -- --agent claude
```

Use `--agent cursor`, `--agent codex`, or `--agent all` to select one or several
IDEs. The installer sets up:

- the `specgate` CLI configuration for this deployment;
- focused SpecGate Skills;
- IDE hooks and project rules.

Coding IDE agents use the CLI for SpecGate work. They do not need local MCP
configuration for the handoff loop.

## 6. Verify the connection

```bash
specgate doctor
specgate status
specgate work list
```

`status` shows the governance board overview and work needing attention.
`status` and `work list` use the selected workspace by default; pass
`--all-workspaces` when you intentionally want the global view.

## 7. Run your first governed work item

The core loop is: create work with acceptance criteria, hand the approved
context to your coding agent, then return evidence for review.

Create a quick work item with explicit acceptance criteria:

```bash
specgate work create-quick "Add healthcheck endpoint" \
  --ac "GET /healthz returns 200 when the service is up" \
  --ac "Healthcheck is covered by an automated test"
```

Note the work-item key in the output, then read its Context Pack — the
implementation brief your coding agent works from:

```bash
specgate work context <work-ref>
```

Implement the change in your repository (or let your IDE agent do it — the
installed SpecGate skills drive these same commands). Then scaffold the
completion report; it prefills one entry per acceptance criterion:

```bash
specgate delivery report <work-ref> --init
```

Edit `completion.json` — fill in the summary, checks you ran, affected files,
and evidence for each criterion — then submit the whole delivery tail (report →
gates → review → verdict) in one command:

```bash
specgate delivery submit <work-ref> --file completion.json
specgate delivery status <work-ref> --detail
```

If the review verdict fails, fix the named gap, update `completion.json`, and
run `delivery submit` again.

## What to explore next

- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md)
- [Governance and gates](concepts/governance-and-gates.md)

## If something goes wrong

### Docker is unavailable

Start Docker, verify `docker compose version`, then run `specgate init` again.
Setup is safe to repeat.

### A service is unhealthy

```bash
specgate local-status
specgate doctor
```

For container details, continue with
[Operate SpecGate](guides/operate-specgate.md).

### Your IDE runs on another machine

`localhost` refers to the machine running the IDE agent. Put Doc Registry on a
host reachable from that machine and configure:

```bash
specgate config set server http://<reachable-registry>
specgate doctor
```

### Model-backed actions fail

The stack runs without a model, but assistive and LLM-gate features cannot. Check
that the provider key is configured with `specgate model set`, the value is
current, and the provider has quota. See
[Configure models](guides/configure-models.md).

## Source checkout?

If you plan to modify SpecGate itself, use
[Contributor setup](guides/contributor-setup.md). The repository’s `make setup`
workflow is for contributors, not the normal product quickstart.
