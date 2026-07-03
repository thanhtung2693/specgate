# Install SpecGate in your coding IDE

Use this guide to install or refresh the SpecGate IDE files for Codex, Claude
Code, and Cursor.

The plugins give coding agents focused SpecGate skills, hooks, and rules. They
use the `specgate` CLI for all product operations.

## Before you start

Install and configure the CLI:

```bash
specgate doctor
```

If `doctor` fails because no server is configured:

```bash
specgate config set server http://localhost:8080
specgate doctor
```

## Install from the CLI

Interactive install:

```bash
specgate plugins install
```

Choose one or more IDEs from the checkbox list. Restart the selected IDEs after
installation.

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

## Install through the public shell installer

Use the public installer when the `specgate` CLI is not installed yet or when
you are piping setup into a fresh machine:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh
```

The installer:

1. asks which IDEs to configure;
2. installs or locates the `specgate` CLI;
3. delegates IDE file writes to `specgate plugins install`.

For automation:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh -s -- --agent all
```

## Choose global or project-local scope

Default install is user-global. It writes files under your home directory and
works across repositories.

Use project-local scope only when a repository should vendor its SpecGate IDE
files:

```bash
specgate plugins install --project-local
specgate plugins doctor --project-local
```

## What is written

| IDE | User-global location | Purpose |
|---|---|---|
| Codex | `~/.codex/plugins/specgate` | native plugin package, hooks, focused skills |
| Codex | `~/.agents/plugins/marketplace.json` | personal marketplace entry for `specgate@personal` |
| Codex | `~/.codex/config.toml` | enables the personal SpecGate plugin |
| Claude Code | `~/.claude/skills/specgate` | native plugin package, hooks, focused skills |
| Cursor | `~/.cursor/rules/using-specgate.mdc` and `~/.cursor/skills/*` | rule and focused skills |

The focused skills are:

- `using-specgate`
- `setting-up-specgate-project`
- `checking-spec-readiness`
- `shaping-work`
- `picking-up-work`
- `implementing-work`
- `completing-delivery`

## Refresh an existing install

```bash
specgate plugins install
specgate plugins doctor
```

Or refresh everything with the CLI updater:

```bash
specgate update
```

Restart the IDE after refresh. Some IDEs cache plugin files until restart.

## Remove plugin files

Use the safe uninstall path:

```bash
specgate uninstall
```

Leave local data unchecked if you only want to remove CLI config and IDE plugin
files. Artifact/spec data remains in Docker volumes unless you select local data
removal or run `specgate uninstall --purge-data --yes`.

## Troubleshooting

### `plugins doctor` reports missing files

Run the installer again:

```bash
specgate plugins install
specgate plugins doctor
```

Then restart the IDE.

### Codex still sees old skills

Restart Codex. If the warning mentions a stale Codex plugin cache, reinstall and
restart:

```bash
specgate plugins install --agent codex
```

### The installer targets the wrong server

Set the server URL before installing:

```bash
specgate config set server http://localhost:8080
specgate plugins install
```

Or pass a registry URL explicitly:

```bash
specgate plugins install --registry http://localhost:8080
```

## Related

- [Quickstart](../quickstart.md)
- [Use the SpecGate CLI](cli-workflow.md)
- [Use SpecGate with a coding agent](coding-agent-workflow.md)
