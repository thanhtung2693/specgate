# Quickstart

This tutorial starts SpecGate locally and runs one governed work item. By the
end, you will have a running stack, a selected local user and workspace, an IDE
plugin install, and one delivery review flow.

## What you need

- macOS, Linux, or Windows with WSL2.
- Docker with Docker Compose v2.
- Network access to download the CLI and container images.

You do not need a source checkout, Go, Node.js, Python, or a model API key.
Server-side model features are optional.

> **Windows:** run the install script and CLI from WSL2. Native Windows binaries
> are also published on the GitHub releases page.

## 1. Install the CLI

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

Check that your shell can find it:

```bash
specgate --help
specgate --version
```

If the command is missing, add the install directory printed by the installer to
your `PATH`. The default is usually `~/.local/bin`.

## 2. Start SpecGate

```bash
specgate init
```

Follow the prompts. The interactive setup:

- chooses a deployment directory, `~/.specgate` by default;
- downloads the release Compose bundle;
- creates environment files and a settings encryption key;
- starts the services;
- asks for your local workspace, display name, username, and optional email;
- asks whether to seed demo data;
- asks whether to install IDE plugins for Codex, Claude Code, and Cursor.

For a seeded non-interactive install without extra choices:

```bash
specgate init --seed --no-input
```

For normal local setup, prefer interactive mode: it uses checkbox prompts for
demo data and IDE plugin choices.

## 3. Verify the stack

```bash
specgate local-status
specgate doctor
```

Default local endpoints:

| Service | URL |
|---|---|
| Web UI | `http://localhost:3000` |
| Doc Registry and Swagger | `http://localhost:8080` |
| Governance-ops | `http://localhost:2024` |

`doctor` checks CLI/server compatibility and required capabilities. If you set
custom ports in the deployment `.env` before startup, `init` saves the matching
Doc Registry URL in CLI config.

## 4. Connect your IDE

If you skipped plugin install during `init`, run:

```bash
specgate plugins install
specgate plugins doctor
```

Interactive mode shows a checkbox list for Cursor, Codex, and Claude Code. For
automation:

```bash
specgate plugins install --agent all --no-input
```

Restart selected IDEs after installing plugins so the new skills, hooks, and
rules load.

## 5. Optional: configure a model

SpecGate can run without a server-side model. The deterministic core still
stores artifacts, resolves policy, creates Context Packs, and records delivery
evidence.

Configure a model when you want server-side summaries, route suggestions,
semantic readiness gates, or model-backed delivery review:

```bash
specgate model set
```

The guided setup asks for provider, model, and API key. For a non-interactive
OpenAI setup:

```bash
specgate model set --provider openai --api-key <your-key>
```

See [Configure models](guides/configure-models.md) for provider details.

## 6. Run one governed work item

Create a quick work item with acceptance criteria:

```bash
specgate work create-quick "Add healthcheck endpoint" \
  --ac "GET /healthz returns 200 when the service is up" \
  --ac "Healthcheck is covered by an automated test"
```

Read the Context Pack:

```bash
specgate work context <work-ref>
```

Coding IDE agents use the CLI through the installed SpecGate skills. Implement
the change yourself or ask your IDE agent to pick up the Context Pack.

Scaffold completion evidence:

```bash
specgate delivery report <work-ref> --init
```

Edit `completion.json`. Fill in:

- summary;
- affected files;
- checks you ran;
- per-criterion evidence.

Submit report, gates, delivery review, and status in one command:

```bash
specgate delivery submit <work-ref> --file completion.json
specgate delivery status <work-ref> --detail
```

If review fails, fix the named gap, update evidence, and run `delivery submit`
again.

## 7. Remove a trial install

Safe default:

```bash
specgate uninstall
```

Interactive mode shows a checklist. Leave local data unchecked to keep
artifacts, specs, work items, settings, and evidence.

Default local data locations:

| Docker volume | Contents |
|---|---|
| `postgres-data` | artifact metadata, work items, features, settings, evidence, gate history |
| `doc-registry-data` | artifact/spec document contents under `/data/blobs` |

To purge local data in automation, back up first, then run:

```bash
specgate uninstall --purge-data --yes
```

This removes the deployment directory, SpecGate-managed containers, volumes,
networks, and service images.

## Troubleshooting

### Docker is unavailable

Start Docker, verify Compose, then run init again:

```bash
docker compose version
specgate init
```

### A service is unhealthy

```bash
specgate local-status
specgate doctor
```

For container logs, see [Operate SpecGate](guides/operate-specgate.md).

### Your IDE runs on another machine

`localhost` refers to the IDE-agent machine. Put Doc Registry on a reachable
host and configure:

```bash
specgate config set server http://<reachable-registry>
specgate doctor
```

### Model-backed actions fail

The stack still works without a model, but LLM-backed gates and assistive
actions cannot run. Check the provider key, model id, and provider quota.

## Next steps

- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [Use the SpecGate CLI](guides/cli-workflow.md)
- [Operate SpecGate](guides/operate-specgate.md)
- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md)
