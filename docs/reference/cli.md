# CLI reference

The `specgate` CLI is the stable command surface for users, automation, and
coding IDE agents.

For exact flags in your installed version:

```bash
specgate <command> --help
```

## Server selection

Highest precedence first:

1. `--server <url>`
2. `SPECGATE_SERVER`
3. saved user configuration
4. `http://localhost:8080`

Set the saved URL:

```bash
specgate config set server http://localhost:8080
```

## Output modes

| Mode | Selection | Use |
|---|---|---|
| Human | default | interactive terminal work |
| Plain | `--plain` | stable text without color or prompts |
| JSON | `--json` | automation and IDE tools |

Human output uses ANSI color, Unicode symbols, section headers, and compact
progress summaries for terminal readability. `--plain` and `--json` never
include color escapes or Unicode dashboard glyphs; `--json` implies `--plain`
and `--no-input`.

Other global flags:

- `--version` — print the installed CLI version;
- `--yes` — accept confirmations;
- `--no-input` — fail instead of prompting;
- `--timeout <duration>` — request timeout;
- `--json-progress` — emit progress events before final JSON output.

## JSON envelope

Success:

```json
{
  "schema_version": "specgate.cli/v1",
  "command": "work.show",
  "ok": true,
  "data": {}
}
```

Failure:

```json
{
  "schema_version": "specgate.cli/v1",
  "command": "work.show",
  "ok": false,
  "error": {
    "code": "not_found",
    "message": "work item not found",
    "details": {}
  }
}
```

JSON mode does not mix prose into stdout.

## Exit codes

| Code | Meaning |
|---:|---|
| `0` | success |
| `1` | governance requirement failed |
| `2` | usage or validation error |
| `3` | resource not found |
| `4` | conflict or stale state |
| `5` | service unavailable |
| `6` | client/server incompatibility |

## Command lanes

The root help groups commands by audience. Existing commands remain compatible,
but new users should start with the core workflow lane.

### Core workflow

| Family | Purpose |
|---|---|
| `status` | Show selected-scope board counts, attention count, and next action |
| `stats` | Show a governance-value readout — reviewed items, first-pass yield, pre/post-build catches, rework, ambiguity saves, cycle time, and a recent-catches ledger; `--days N` sets the window, `--all-workspaces` clears the workspace filter |
| `work` | List the attention queue, resolve, create, inspect, archive, read context, and explain work policy |
| `feature` | List and inspect governed features — look up an existing feature's key before publishing so a new artifact links to it instead of creating a duplicate |
| `artifact` | Publish, inspect, read, and propose artifacts; decide as a human reviewer with `artifact approve` / `artifact request-changes` (optional `--note`), and work the proposal review queue with `artifact proposals [approve\|reject <session-id>]`; `artifact show` accepts a unique id prefix from `artifact list` |
| `gates` | All quality-gate operations: work-item LLM gates (`run`, `status`, `history`), artifact readiness checks (`check`), and artifact gate tasks (`tasks`) |
| `delivery` | Report implementation and run delivery review; `delivery report --init` scaffolds a completion.json from the work item's acceptance criteria, and `delivery submit` runs the whole tail (report → gates → review → status) in one command |

`work create-quick <title>` uses the title as the description when
`--description` is omitted, so the shortest quickstart form still satisfies the
server contract.

### Setup and identity

| Family | Purpose |
|---|---|
| `doctor` | Diagnose CLI/server compatibility and capability health |
| `config` | Save CLI configuration |
| `version` | Print the installed CLI version; equivalent to `specgate --version` |
| `model` | Configure the local model provider, model, and API key (no UI) |
| `user` | List local users and show the selected user |
| `workspace` | List workspaces, show the selected workspace, and select a workspace |
| `plugins` | Install and verify Codex, Claude Code, and Cursor IDE plugin files |
| `skill` | Inspect user-defined Skills |
| `open`, `update`, `uninstall` | Open the web UI — `open` alone opens the base URL; `open <work-ref>`, `open reviews\|artifacts\|work`, and `open --artifact <id>` deep-link to the matching page — refresh setup, or remove user-local setup |

The selected local user and workspace are stored in CLI config. They are used
for attribution and default workspace filtering, not authentication: quick work
items receive `created_by` and `workspace_id`, archive actions use the selected
username as the actor, and `status` / `work list` filter to the selected
workspace unless `--all-workspaces` is passed. Artifact publish bodies do not
carry `created_by` — the server attributes CLI-published packages itself, and
a body that includes the field is rejected.

### Local stack

| Family | Purpose |
|---|---|
| `init`, `up`, `down`, `local-status` | Manage a local SpecGate deployment |

`specgate uninstall` removes user-local SpecGate setup. In an interactive
terminal it shows a checkbox list:

| Choice | Effect |
|---|---|
| IDE plugin files | Remove SpecGate files from Cursor, Codex, and Claude Code user plugin locations |
| Local data | Remove Docker volumes and the deployment directory |
| Docker images | Remove SpecGate service images |

By default, the command removes CLI config and IDE plugin files, stops a
CLI-managed local stack when present, and keeps artifact/spec data. Artifact
metadata, work items, evidence, settings, and gate history live in Postgres.
Artifact/spec file contents live in the Doc Registry blob volume.

For automation, pass `--purge-data --yes` only after backing up data you want to
keep:

```bash
specgate uninstall
specgate uninstall --purge-data --yes
```

`--purge-data --yes` removes:

- SpecGate-managed containers;
- SpecGate-managed Docker volumes;
- SpecGate-managed Docker networks;
- the deployment directory;
- SpecGate service images referenced by that deployment.

Release containers, volumes, and networks are labelled with
`org.specgate.managed=true` and `org.specgate.project=<project>`. Uninstall uses
both labels for cleanup so purging one local stack does not remove another.
Image cleanup removes the selected deployment's SpecGate service images, not
shared base images such as Postgres or Redis.

### Advanced governance

| Family | Purpose |
|---|---|
| `policy` | Inspect built-in governance levels (`policy list`) |

For work-item policy, use `specgate work policy <ref>`.

## Interaction

When a work reference is optional, the CLI may show work needing attention and
ask you to choose. Confirmation prompts (for example before `gates run` and
`delivery review`) appear only in interactive terminal sessions; non-interactive
runs (`--json`, `--no-input`, piped stdin) proceed without prompting; `--yes`
is optional there.

Automation should pass all required values and use `--json --no-input`.

## Token-conscious output

`specgate work context` returns the full Context Pack because coding agents need
the complete approved implementation contract. Other potentially large commands
default to references or summaries:

- `specgate artifact files <id> <path...>` returns file path, size, and URL when
  available; add `--content` only when the file body is needed.
- `specgate artifact publish --file artifact.json` accepts local `source_file`
  paths or absolute local `file_url` entries in `documents[]`; the CLI reads
  those files and sends raw UTF-8 content, not base64, to avoid bloating IDE
  prompts.
- `specgate skill list --json` omits full prompts; add `--include-prompt` only
  when an agent needs the rubric body.
- `specgate delivery submit <ref> --file completion.json` replaces the
  report → gates → review → status command tail with one call and one combined
  `{report, gates, review, status}` envelope, so agents spend one round-trip
  instead of four. Scaffold the completion file with
  `specgate delivery report <ref> --init` — it prefills `criterion_id` and text
  for every acceptance criterion so agents do not have to re-derive them.

## Compatibility

`specgate doctor` checks connection and capability compatibility:

```bash
specgate doctor --json
```

An exit code of `6` means the CLI and server disagree on a required contract.
Update the CLI or deployment before continuing.

In human/plain output, `doctor` also shows a "Local stack" section with the
running compose services when a CLI-managed deployment exists. `local-status`
is the script-facing command for the same data.

Use `--fix` to repair common local setup failures:

```bash
specgate doctor --fix
```

In an interactive terminal, `doctor --fix` shows a checkbox list before changing
the local machine. The first repair action starts or repairs the CLI-managed
Docker services and then reruns the same doctor checks. For automation, combine
it with `--yes`:

```bash
specgate doctor --fix --yes
```

For human/plain output, server-backed commands also warn when the connected
server recommends a different CLI version. Use the built-in updater:

```bash
specgate update
```

The updater installs the CLI from the public GitHub release installer, refreshes
IDE setup for Codex, Claude Code, and Cursor from the public plugin registry,
and updates the local Compose bundle/images when a CLI-managed deployment is
present. The Compose step pulls whatever services the release bundle ships,
including Doc Registry, agents, and the web UI.

If the connected server does not recommend a newer CLI, released CLI builds also
check GitHub releases in human/plain output and warn when a newer public release
exists. The GitHub check includes prereleases, caches the result for 24 hours
under the user cache directory, never affects the command exit code, and is
disabled in `--json`, `CI=true`, `dev` builds, or when
`SPECGATE_NO_UPDATE_CHECK=1`.

IDE plugin setup can also be managed directly:

```bash
specgate plugins install
specgate plugins doctor
```

`plugins doctor` compares installed files against the configured plugin registry
(the connected deployment by default; pass `--registry` for another source).
Missing files fail the check; stale versions or a stale Codex plugin cache warn
with the reinstall/restart action to take.

Interactive `plugins install` and `plugins doctor` show a checkbox list for
Cursor, Codex, and Claude Code when `--agent` is omitted. Scripts can still pass
`--agent all`, `--agent codex`, or a comma-separated subset.

`specgate init` offers the same plugin install interactively, including the IDE
target checkbox list. For automation, install plugins after init with
`specgate plugins install --agent all --no-input`.

Use `--project-local` on those commands only when the current repository should
vendor its IDE plugin files.

When multiple local stacks exist, confirm or set the server URL first:
`specgate doctor`, `specgate local-status`, then
`specgate config set server http://localhost:<port>` if needed.

## Related guides

- [Use the SpecGate CLI](../guides/cli-workflow.md)
- [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md)
- [Operate SpecGate](../guides/operate-specgate.md)
