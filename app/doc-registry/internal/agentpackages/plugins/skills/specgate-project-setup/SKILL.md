---
name: specgate-project-setup
description: Use when SpecGate is being initialized or configured for a repository, its workspace binding is missing, IDE plugins need installation or refresh, or setup diagnostics fail.
---

# Setting Up SpecGate

Apply the [router operating contract](../specgate-router/SKILL.md#operating-contract).
This phase configures SpecGate; it does not create specifications or product
work.

## 1. Inspect the current state

```bash
specgate --version
specgate doctor --json
```

Use the doctor's reported state and recovery action. In an existing topology,
follow that recovery action instead of rerunning initialization or switching
mode. Initialization is required only when doctor reports no initialized
topology.

Completion criterion: the installed CLI version and every failing doctor check
are recorded, or doctor confirms the current topology is healthy.

## 2. Initialize only when required

The user chooses Local or Full mode; never infer that product decision from
Docker, a URL, or installed software. In an interactive terminal, run exactly
the chosen mode:

```bash
specgate init --mode local
specgate init --mode full
```

For noninteractive Local initialization, collect the user's identity values and
run:

```bash
specgate init --mode local --no-input \
  --workspace-name "<workspace>" \
  --display-name "<display name>" \
  --username "<username>"
```

Use `specgate init --help` for Full or non-default noninteractive deployment
options. Do not purge, replace, or migrate existing data as part of setup.

Completion criterion: the chosen mode is initialized, or the exact failed
command and its recovery action are reported.

## 3. Select and bind the workspace

```bash
specgate user current --json
specgate workspace current --json
specgate workspace bind
```

If identity or workspace selection is missing or ambiguous, stop for the user
to choose it. Bind only when the current repository binding is missing,
incorrect, or explicitly requested; a plugin-only refresh leaves a correct
binding unchanged.

Completion criterion: `specgate workspace current --json` identifies the
intended workspace and the repository binding is correct or deliberately left
unchanged.

## 4. Install or refresh the selected IDE integration

The user chooses the IDE and whether its files are user-global or
project-local. Preview the exact scope, then install it noninteractively:

```bash
specgate plugins install --agent <codex|claude|cursor> --dry-run --no-input
specgate plugins install --agent <codex|claude|cursor> --no-input
```

Add `--project-local` to both commands only when that scope was selected. Do
not install every IDE or change scope merely because an executable is present.

Completion criterion: the preview and installation name the user-selected IDE
and scope, and no unselected integration is modified.

## 5. Verify and hand off

```bash
specgate doctor --json
specgate workspace current --json
specgate plugins doctor --agent <codex|claude|cursor> --json
```

Add `--project-local` to plugin doctor when that is the installed scope. Tell
the user to restart the selected IDE; file verification does not prove a
running IDE has reloaded the plugin.

Completion criterion: SpecGate and plugin doctor are healthy for the selected
topology, workspace, IDE, and scope; any required restart is explicit. On
failure, report the exact failed command and recovery action instead of an
advisory repository map.
