# Install SpecGate in your coding IDE

Use this guide to install or refresh SpecGate for Codex, Claude Code, and
Cursor. Use one installation method per IDE.

The IDE integration gives coding agents focused SpecGate skills and, where the
IDE supports them, hooks or rules. It uses the `specgate` CLI for all product
operations.

## Before you start

Install the CLI first:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

For the default Local workflow, initialize the local store:

```bash
specgate init
specgate doctor
```

Local uses the same focused IDE plugin as Full without contacting a registry,
Docker, or server; the CLI embeds its files.

For Full appliance or an existing server, first verify the configured server:

```bash
specgate doctor
```

If no server is configured, set one before installing Full-mode plugins:

```bash
specgate config server http://localhost:3000/api/doc-registry
specgate doctor
```

## Choose an installation method

The supported paths are:

1. **Codex or Claude Code plugin manager**
2. **SpecGate CLI** for Codex, Claude Code, or Cursor
3. **skills.sh** for agent-led setup

Do not combine the plugin manager and CLI methods for the same IDE.

## Install through a native plugin manager

The repository can act as a Codex or Claude Code marketplace. SpecGate is not
listed in an official plugin directory.

For Codex, add the repository marketplace:

```bash
codex plugin marketplace add thanhtung2693/specgate
```

Then start Codex, open `/plugins`, and install `specgate@specgate`. Codex owns
the cached package and its enablement. Use `/plugins` again to update, disable,
or remove it; `codex plugin marketplace upgrade specgate` refreshes the
repository marketplace catalog.

For Claude Code, add the GitHub repository as a marketplace, then install the
plugin from the shell:

```bash
claude plugin marketplace add thanhtung2693/specgate
claude plugin install specgate@specgate
claude plugin list
```

Refresh or remove the Claude plugin through the same manager:

```bash
claude plugin update specgate@specgate
claude plugin uninstall specgate@specgate
```

Use the same manager to update or remove the plugin. `specgate plugins install`
detects native ownership and stops before writing duplicate IDE files.

## Start from skills.sh

The skills.sh entry is an agent-led bootstrap. It installs instructions only,
not the SpecGate CLI or complete IDE plugin:

```bash
npx skills add https://github.com/thanhtung2693/specgate --skill specgate
```

Start a new IDE-agent conversation and ask it to **“Set up SpecGate.”** The
bootstrap checks for the CLI, shows the official installer when needed, and
waits for approval before running it.

The bootstrap and complete plugin must not become two managers for the same
root skill. Immediately before the approved plugin install, remove the
skills.sh bootstrap from the scope where skills.sh reported it:

```bash
# Project scope
npx skills remove specgate -y

# Global scope
npx skills remove specgate -g -y
```

The already-loaded bootstrap can finish the current conversation after its
file is removed. It previews and installs the complete plugin, verifies it,
then asks you to restart the IDE. The SpecGate CLI becomes the sole manager for
all four focused skills and any supported hooks or rules.

`specgate plugins install` recognizes an official skills.sh bootstrap and stops
before writing files. Its error includes the exact removal and retry commands.
SpecGate never edits or deletes skills.sh lock files; the skills.sh removal
command preserves unrelated skills and lock entries.

## Install through the SpecGate CLI

Interactive install:

```bash
specgate plugins install
```

Plugin packages and executable hooks never use a repository's committed
`.specgate/config` server. The installer uses an explicit `--registry`, then
`--server`/`SPECGATE_SERVER`, then the user's saved server, so opening another
repository cannot redirect a global IDE-plugin install.

On a new setup, this command works before `specgate init`; you do not need to
start Full mode first.

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
are preserved. Project-local installs do not change Codex configuration or
plugin-manager files.

## Refresh an existing install

```bash
specgate plugins install
specgate plugins doctor
```

Or refresh everything with the CLI updater:

```bash
specgate update
```

In Local mode, `update` refreshes the CLI and IDE plugins installed by it, then
skips the Full appliance. Update native plugins with their plugin manager.

Restart the IDE after refresh. Some IDEs cache plugin files until restart.

## Remove plugin files and data

Use the safe uninstall path:

```bash
specgate uninstall
```

In Local mode, `uninstall` removes CLI configuration and globally installed
managed plugin files while preserving Local SpecGate data. Project-local plugin
files are preserved, as are plugins installed by an IDE's own plugin manager.
To delete Local SpecGate data too:

```bash
specgate uninstall --purge-data --yes
```

Cleanup warns first and removes only SpecGate-owned data. In Full mode, the
same flag removes the managed appliance and its data while leaving Docker image
cache intact. See the [CLI reference](../reference/cli.md) for exact scope and
flags.

## Troubleshooting

### `plugins install` reports a native marketplace conflict

Keep the native plugin, or remove it through that manager before retrying the
CLI install. Do not delete marketplace cache files by hand.

### `plugins doctor` reports missing CLI-managed files

For a CLI-managed install, run the repair command shown by `plugins doctor`. It
includes the selected agent and `--project-local` when needed. For example:

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
