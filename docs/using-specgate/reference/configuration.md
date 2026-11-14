# Configuration

This reference covers the Local CLI configuration and project settings.
For operating and backup guidance, see [Operate SpecGate](../guides/operate-specgate.md).

## CLI

`specgate init` creates a Local user and workspace in SQLite and binds the
current Git checkout. For unattended setup, supply explicit identity values:

```bash
specgate init --no-input \
  --workspace-name "My workspace" \
  --display-name "Jane Doe" \
  --username jane
```

Local initialization can optionally install IDE integration files in the same
command. Use `--install-plugins --plugin-agent <agent>` and choose
`--plugin-scope global|project`; `global` is the existing default. Plugin
ownership and package safety are previewed before Local state changes.
Project-scoped setup must run from the Git repository root so relative IDE
paths cannot land in a nested directory. If plugin writing later fails, Local
identity, workspace, store, and repository setup remain complete and the error
provides the plugin install command to retry. Both retry and successful
verification commands preserve `--project-local` when project scope was selected.

Local artifact preview and publication also read optional project-owned
`.specgate/rubrics/` Markdown instructions from the selected Git root, including
when invoked in a subdirectory. Only the four existing semantic gate filenames
are supported; no directory or files are created automatically. Effective text
is frozen at publication, so edits require a fresh preview and new artifact
version. See
[Local project rubrics](../guides/customize-governance.md#add-local-project-instructions)
for bounds, errors and source ownership.

### Previous installations

A configuration that selects the retired remote setup is preserved. Use a prior
compatible CLI to access that deployment. For independent Local
initialization, follow [the recovery steps](../guides/operate-specgate.md#previous-installations).

Local state defaults to the OS SpecGate config location. Use `--local-dir` at
initialization to select another store, or `SPECGATE_LOCAL_DIR` for a
non-persisting Local-command override. `SPECGATE_CONFIG_PATH` selects an
isolated CLI config file for automation and dogfood sessions. Local `doctor`
reports the effective store directory, including environment or project-binding
overrides, without persisting that selection to global configuration.

`specgate ui` optionally starts a loopback-only review listener inside the CLI
process. Its Local governed route is:

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
# Use review_id from the returned status (or change status --json).
specgate --yes change accept <work-ref> --review-id <review-id>
specgate audit <work-ref> --json
```

Artifact documents are immutable (1 MiB per document, 10 MiB per package).
Local readiness combines deterministic checks with frozen Local IDE gate tasks;
it remains `not_run` until required semantic results are submitted and is
distinct from human approval. Semantic task results are `agent_attested`. The authoritative unfinished-task receipt
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
`AGENTS.md`. `specgate doctor` treats those plugins as optional: a missing
plugin does not make CLI-only Local mode unhealthy. It reports repository
binding, `sh` availability, and plugin-file health as distinct diagnostics.
Plugin files on disk do not prove an already-running IDE session loaded them;
restart the IDE and verify from a new session.
Plugin commands use their embedded package before initialization too.
Use `--workspace <slug-or-id>` or `SPECGATE_WORKSPACE` for a one-command Local
workspace override; neither changes the stored selection.

CLI config stores the selected Local user and workspace. It may also store project-scoped workspace
bindings keyed by a local Git root path. `specgate init` creates the binding
for the Git checkout where initialization runs. When a command runs from a bound
checkout, that workspace overrides the global workspace for attribution and
default filtering. In a different Git checkout, workspace-scoped commands stop
until it is bound, given a repo default, or run with an explicit workspace
override. Outside Git checkouts, the global workspace remains available. A
malformed global config fails closed before operational commands
modify local state; repair or move that file aside and run
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
workspace select <slug>` saves the global workspace using an explicit slug
while preserving project bindings.
Use `workspace bind` when automation should bind a project. `specgate user
logout` clears both the global workspace and project-scoped workspace bindings.
`specgate user login` records the user and global workspace and leaves existing
project bindings alone, so signing in never unbinds other checkouts; a binding
whose workspace no longer resolves fails in that project with its own recovery
command.
`specgate transfer inspect` and `transfer receive` work without resolving
an active config. `transfer activate` uses the explicitly selected config
file and the supplied Git root. It changes only that root's Local store and
workspace binding, preserving global settings, other project bindings and
legacy field values. It can initialize an empty Local config. A selected
Full config or malformed config requires repair or selection of a separate
Local config. A store or workspace override that points elsewhere blocks
activation. When `SPECGATE_LOCAL_DIR` is set, supply the receiving directory's
absolute path without symlink components. Activation compares the config with the preview before saving it
atomically. Concurrent writes from unrelated tools remain possible.
CLI environment variables:

| Variable | Purpose |
|---|---|
| `SPECGATE_WORKSPACE` | Overrides the project/global workspace selection for one command when `--workspace` is omitted. |
| `SPECGATE_LOCAL_DIR` | Overrides the selected Local SQLite state directory for one command session. |
| `SPECGATE_CONFIG_PATH` | Overrides the per-user CLI config path for an isolated session. |
| `SPECGATE_NO_UPDATE_CHECK` | Set to `1`, `true`, `yes`, or `on` to disable the public GitHub release freshness check. |
| `SPECGATE_ACCESSIBLE` | Set to `1` to render interactive prompts in accessible mode, which screen readers can follow. |
| `CI` | When truthy, suppresses human-facing public update checks. |

`SPECGATE_TEST_REPORT` is deliberately absent from this table. For an opted-in
Local JUnit check, SpecGate creates it only in the child process environment for
one confirmed `--run-checks` invocation. Do not place it in `.env`, shell
profiles, or committed project configuration: the generated path is transient,
must be absent before the run, and must remain in the checkout's ignored
`.specgate/` directory.

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
  after explicit overrides and the per-user binding.

  ```json
  {
    "workspace": "platform"
  }
  ```

This file can supply a workspace default. Legacy remote fields have no
active transport. Keep secrets and per-user identity out of committed config.
The repo default `workspace` is honored only when you have no per-user project
binding; your binding always wins.

To commit the shared config, force-add it past the `.specgate/` ignore rule:

```bash
git add -f .specgate/config
```

## Related

- [Operate SpecGate](../guides/operate-specgate.md)
- [Trust and security](../concepts/trust-and-security.md)
