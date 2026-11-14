---
name: specgate-project-setup
description: Use when SpecGate is being initialized or configured for a repository, its workspace binding is missing, IDE plugins need installation or refresh, or setup diagnostics fail.
---

# Setting Up SpecGate

Apply the [SpecGate operating contract](../specgate/SKILL.md#operating-contract).
This phase configures SpecGate; it does not create specifications or product
work.

## 1. Ensure the CLI is available

Use the host shell's command lookup before running any SpecGate command:

```bash
command -v specgate
```

```powershell
Get-Command specgate -ErrorAction SilentlyContinue
```

When lookup succeeds, run `specgate --version` and continue to inspect the
current state.

Installing from skills.sh does not install the SpecGate CLI; it provides agent
instructions only. If the command is missing on macOS, Linux, or WSL2,
show the user the exact installer command and wait for explicit approval before
running it:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

A general request to set up or initialize SpecGate is not approval to download
and execute the installer. On native Windows, offer WSL2 with the command above
or direct the user to
`https://github.com/thanhtung2693/specgate/releases/latest`; do not invent a
PowerShell installer. After installation, run `specgate --version`. If lookup
still fails, report the install path and PATH recovery printed by the installer.

Completion criterion: the installed CLI version is recorded, or setup is
paused for explicit installer approval or native Windows installation.

## 2. Inspect the current state

```bash
specgate doctor --json
```

Use the doctor's reported state and recovery action. Initialize only when doctor
reports Local setup missing. For a retired remote configuration, preserve it and
use the prior compatible CLI for that deployment. For a new Local workspace,
set `SPECGATE_CONFIG_PATH` to a separate file, unset `SPECGATE_SERVER`, and
run `specgate init` outside the legacy checkout. Then return and bind it.

Completion criterion: every failing doctor check is recorded, or doctor
confirms the current topology is healthy.

## 3. Initialize only when required

In an interactive terminal:

```bash
specgate init
```

For noninteractive Local initialization, collect the user's identity values and
run:

```bash
specgate init --no-input \
  --workspace-name "<workspace>" \
  --display-name "<display name>" \
  --username "<username>"
```

Use `specgate init --help` for other Local setup options. Preserve existing data.

Completion criterion: Local setup is initialized, or the exact failed
command and its recovery action are reported.

## 4. Select and bind the workspace

```bash
specgate user current --json
specgate workspace current --json
```

If identity or workspace selection is missing or ambiguous, stop for the user
to choose it. `specgate init` binds the checkout where it runs. If
`workspace current` reports this checkout as unbound, bind it with:

```bash
specgate workspace bind
```

Bind only when the current repository binding is missing, incorrect, or
explicitly requested; a plugin-only refresh leaves a correct binding unchanged.

Completion criterion: `specgate workspace current --json` identifies the
intended workspace and the repository binding is correct or deliberately left
unchanged.

## 5. Install or refresh the selected IDE integration

Read the session ownership context. If the IDE plugin manager owns SpecGate,
skip plugin install and doctor for that IDE. Use that manager for plugin changes.

Otherwise, the user chooses the IDE and whether its files are user-global or
project-local. Preview the exact scope, then install it noninteractively:

```bash
specgate plugins install --agent <codex|claude|cursor> --dry-run --no-input
specgate plugins install --agent <codex|claude|cursor> --no-input
```

Add `--project-local` to both commands only when that scope was selected. Do
not install every IDE or change scope merely because an executable is present.

Completion criterion: one owner, selected IDE and scope only.

## 6. Verify and hand off

```bash
specgate doctor --json
specgate workspace current --json
specgate plugins doctor --agent <codex|claude|cursor> --json
```

Run plugin doctor only for a CLI-installed integration; add `--project-local`
when needed. Otherwise use the IDE plugin manager. Restart the IDE after plugin
changes.

Completion criterion: SpecGate, workspace, and selected IDE integration are
healthy. On failure, report the failed command and recovery action.
