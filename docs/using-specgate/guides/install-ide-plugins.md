# Install SpecGate in your coding IDE

Use this guide to install or refresh the SpecGate IDE files for Codex, Claude
Code, and Cursor.

The IDE integration gives coding agents focused SpecGate skills and, where the
IDE supports them, hooks or rules. It uses the `specgate` CLI for all product
operations.

## Before you start

Install the CLI first:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

For the default Local CLI workflow, initialize the local store, then install the
embedded project-local IDE files for your agent:

```bash
specgate init
specgate doctor
specgate plugins install --agent codex --project-local
specgate plugins doctor --agent codex --project-local
```

Local embeds the complete Codex, Claude Code, and Cursor assets with the same
focused skills as Full—router, setup, preparation, and delivery—without
contacting a registry, Docker, or a server. Replace `codex` with `claude` or
`cursor`, or pass `--agent all`. Restart the selected IDE after installation.

For Full appliance or an existing server, first verify the configured server:

```bash
specgate doctor
```

If no server is configured, set one before installing Full-mode plugins:

```bash
specgate config server http://localhost:3000/api/doc-registry
specgate doctor
```

## Install plugins from the CLI

Interactive install:

```bash
specgate plugins install
```

Plugin packages and executable hooks never use a repository's committed
`.specgate/config` server. The installer uses an explicit `--registry`, then
`--server`/`SPECGATE_SERVER`, then the user's saved server, so opening another
repository cannot redirect a global IDE-plugin install.

Choose one or more IDEs from the checkbox list. The default selection prefers
IDE tools detected on your machine and falls back to all supported agents when
none are detected. Interactive install also asks whether to write global user
files or project-local files. Restart the selected IDEs after installation.

Non-interactive examples:

```bash
specgate plugins install --agent all --no-input
specgate plugins install --agent codex --no-input
specgate plugins install --agent cursor,codex --no-input
```

Verify:

```bash
specgate plugins doctor
```

`plugins doctor` also opens a checkbox list in interactive mode. Scripts can
pass the same `--agent` values.

## Choose global or project-local scope

Default install is user-global. It writes files under your home directory and
works across repositories. In an interactive terminal, `plugins install` asks
for this scope unless `--project-local` is passed.

Use project-local scope only when a repository should vendor its SpecGate IDE
files:

```bash
specgate plugins install --project-local
specgate plugins doctor --project-local
```

Project-local installation updates only the selected IDE's focused SpecGate
skills and, for Cursor, its SpecGate rule. It does not change Codex marketplace
configuration, `.codex/config.toml`, `AGENTS.md`, or unrelated project files.

## What is written

| IDE | User-global location | Project-local location |
|---|---|---|
| Codex | `~/.codex/plugins/specgate`, `~/.agents/plugins/marketplace.json`, and `~/.codex/config.toml` | `.agents/skills/specgate` and `.agents/skills/specgate-*` |
| Claude Code | `~/.claude/skills/specgate` | `.claude/skills/specgate` and `.claude/skills/specgate-*` |
| Cursor | `~/.cursor/rules/using-specgate.mdc`, `~/.cursor/skills/specgate`, and `~/.cursor/skills/specgate-*` | `.cursor/rules/using-specgate.mdc`, `.cursor/skills/specgate`, and `.cursor/skills/specgate-*` |

Global Codex and Claude Code locations contain the native plugin package, hooks,
and focused skills. Project-local Codex and Claude Code locations contain
focused skills only, using the repository paths those IDEs discover directly.

The focused skills are:

- `specgate` — point the agent at the right lifecycle phase
- `specgate-project-setup` — discover a repository's governance conventions
- `specgate-work-preparation` — shape and publish work, then check readiness for handoff
- `specgate-work-delivery` — pick up an approved pack, implement, and report delivery evidence

All installed skill names use `specgate` or the `specgate-` namespace to avoid
colliding with general-purpose IDE skills. The installer rejects empty,
duplicate, or unnamespaced package inventories before writing or removing
anything.

Re-running `plugins install` refreshes those focused skills and removes obsolete
SpecGate-owned skill names and prior versioned Codex cache bundles in the
selected IDE scope. It preserves unowned skills and files.

For a global Codex install, the installer edits only SpecGate-owned
`config.toml` sections. Unrelated settings, ordering, whitespace, and comments
are preserved. When refreshing an older project-local Codex install, it removes
only the legacy SpecGate-owned bundle and registration; unrelated files and
configuration survive.

## Refresh an existing install

```bash
specgate plugins install
specgate plugins doctor
```

Or refresh everything with the CLI updater:

```bash
specgate update
```

In Local mode, `update` refreshes the CLI and already-installed global IDE
plugins from the exact selected release, then skips the Full-appliance step. It
does not inspect, start, or update Docker.

Restart the IDE after refresh. Some IDEs cache plugin files until restart.

## Remove plugin files and data

Use the safe uninstall path:

```bash
specgate uninstall
```

In Local CLI mode, `uninstall` removes CLI configuration and globally installed
managed plugin files while preserving the selected SQLite store. Project-local
plugin files in repositories are preserved; review and remove them through
normal repository file cleanup when no longer needed. To delete the store too,
run:

```bash
specgate uninstall --purge-data --yes
```

This Local cleanup warns first and removes only SpecGate's SQLite database and
journal files, preserving other files in the selected directory. Local mode
rejects the Full-only `--dir` flag and uses the configured SQLite state
directory. It does not use Docker. In Full appliance mode, the same flag removes the managed deployment
directory and SpecGate-managed Docker runtime/data. Container images stay in
Docker's cache.

## Troubleshooting

### `plugins doctor` reports missing files

Run the repair command shown by `plugins doctor`. It includes the selected
agent and `--project-local` when needed. For example:

```bash
specgate plugins install --agent codex --project-local
specgate plugins doctor --agent codex --project-local
```

Then restart the IDE.

### Codex still sees stale skills

Restart Codex. If the warning mentions a stale Codex plugin cache, reinstall and
restart:

```bash
specgate plugins install --agent codex
```

### The installer targets the wrong server

Set the server URL before installing:

```bash
specgate config server http://localhost:3000/api/doc-registry
specgate plugins install
```

Or pass a SpecGate agent-package registry URL explicitly:

```bash
specgate plugins install --registry http://localhost:3000/api/doc-registry
```

## Related

- [Quickstart](../quickstart.md)
- [Use the SpecGate CLI](cli-workflow.md)
- [Use SpecGate with a coding agent](coding-agent-workflow.md)
