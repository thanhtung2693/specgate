# Install SpecGate in your coding IDE

SpecGate’s IDE setup installs the CLI connection, focused Skills, hooks, and
agent rules needed for the governed coding-agent loop. By default, setup is
global to your user account so you install it once and use it across repos.

## Recommended: run the public installer

Run the installer from GitHub:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh
```

The installer asks which IDEs to configure, installs or locates the `specgate`
CLI, and then delegates IDE file writes to `specgate plugins install`.

Restart the selected IDEs after installation so their plugin, Skill, hook, and
rule loaders pick up the new files.

Re-running the installer is safe: it refreshes the installed SpecGate files in
place.

Use `--project-local` only when you intentionally want SpecGate files written
into the current repository.

## Choose one or several IDEs

Install one:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh -s -- --agent claude
```

Install several:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh -s -- --agent cursor,codex
```

Install all supported IDEs:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh -s -- --agent all
```

Use `--dry-run` to inspect changes before writing files:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh -s -- --dry-run
```

Use `--skip-cli` when the CLI is already installed and you only want to refresh
IDE Skills, hooks, and Cursor rules:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh -s -- --agent cursor,codex --skip-cli
```

After the CLI is installed, the equivalent direct command is:

```bash
specgate plugins install --agent cursor,codex \
  --registry https://raw.githubusercontent.com/thanhtung2693/specgate/main
```

Use your deployment URL instead of GitHub only when you intentionally need the
plugin package served by that deployment:

```bash
curl -fsSL http://<your-registry>/plugins/install.sh | sh
specgate plugins install --agent all --registry http://<your-registry>
```

## Claude Code

The instance-aware installer writes the SpecGate instructions, Skills, and hooks
needed by Claude Code.

Use the shell installer for Claude Code as well; it refreshes the focused
Skills and hook settings without requiring a separate plugin command.

The `using-specgate` Skill routes the agent to the smallest lifecycle Skill:
`setting-up-specgate-project` for setup, `preparing-work` for shaping and
readiness, or `delivering-work` for pickup through completion.

For normal work, ask the agent to use `using-specgate` and let it choose the
phase skill. Invoke `setting-up-specgate-project` explicitly when a repository is
new to SpecGate, plugin files were refreshed, or the agent needs to map
canonical docs, tracker mirrors, readiness rules, verification commands, and
domain vocabulary before pickup.

A marketplace plugin manifest is also published for Claude Code plugin
compatibility, but the install script is the supported path for end-user setup.

## Cursor

By default, the installer writes:

- `~/.cursor/rules/using-specgate.mdc`;
- focused Skills under `~/.cursor/skills/`;
- compatible hooks where supported.

Cursor’s agent calls `specgate` commands. It does not need direct SpecGate MCP
tool configuration for coding handoff.

## Codex

By default, the shell installer writes the native Codex plugin package under
`~/.codex/plugins/specgate`, including focused Skills and bundled hooks. It also
reconciles
`~/.agents/plugins/marketplace.json`, and enables `specgate@personal` in
`~/.codex/config.toml`.

Codex reviews newly installed plugin hooks before running them. After restart,
open Codex's hook review prompt/settings and trust the SpecGate SessionStart
hook if you want automatic SpecGate context in new threads.

Plugin marketplace alternative for repository developers:

```bash
codex plugin marketplace add --sparse plugins thanhtung2693/specgate
```

Restart Codex after installing so fresh threads load the plugin skills and
bundled hooks.

## What the installer writes

Depending on the IDE, setup may include:

- server URL in CLI configuration;
- focused lifecycle Skills;
- session-start context;
- hooks that remind the agent to read governed context;
- no general SpecGate MCP credential.

With `--project-local`, the same files are written into the current repository
instead (`.codex/plugins/specgate`, `.cursor/skills/`, `.cursor/rules/`, or
`.claude/skills/specgate/` as applicable). Commit those only if the repo
intentionally vendors its agent setup.

Canonical Skills include:

- `using-specgate`;
- `setting-up-specgate-project`;
- `preparing-work`;
- `delivering-work`.

Use `using-specgate` as the everyday router. The setup skill is an explicit
onboarding command; `preparing-work` covers shaping and publishing artifacts to
readiness, and `delivering-work` covers the implementation arc (pickup, scoped
changes, verification, completion report, delivery review).

## Update an existing installation

```bash
specgate update
```

This updates the CLI and refreshes IDE setup for all supported agents
(Codex, Claude Code, and Cursor) from the connected deployment. Restart any
running IDEs after the update so plugin, Skill, hook, and rule loaders reload
the files.

Before updating, confirm the CLI points at the intended deployment:

```bash
specgate doctor
specgate local-status
```

If your local stack uses a non-default port, set it explicitly, for example
`specgate config set server http://localhost:18080`, before running
`specgate update`.

For machine-readable progress:

```bash
specgate --json --json-progress update
```

## Verify the setup

```bash
specgate doctor
specgate plugins doctor --agent all
specgate status
```

`plugins doctor` checks the installed IDE files against the latest plugin
package served by the connected SpecGate deployment. A missing file fails the
check. A stale installed plugin or stale Codex plugin cache prints a warning
with the next action, usually `specgate plugins install --agent <agent>` and an
IDE restart.

Then ask the IDE agent to inspect a SpecGate work item. It should use:

```bash
specgate work show "$WORK_REF" --json
specgate work context "$WORK_REF" --json
```

If it attempts SpecGate MCP tool calls instead of the CLI, reinstall or update
the plugin files.

## Remove or reinstall

The installer prints files it writes. Remove those global IDE-specific files
manually, or rerun the installer to refresh them.

Before removing files created with `--project-local`, confirm they are not
maintained by your team for other purposes.

## Continue

- [Use SpecGate with a coding agent](coding-agent-workflow.md)
- [CLI reference](../reference/cli.md)
