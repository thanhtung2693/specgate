# Use the SpecGate CLI

The CLI is the fastest way to operate SpecGate, inspect governed work, and give
coding agents a stable machine-readable interface.

## Human mode and interactive prompts

Commands use human-friendly output by default, including terminal color,
status symbols, progress summaries, and checkbox-style delivery criteria where
that helps people scan the governed loop. When a required value is
missing, the CLI may prompt you to:

- select a work item;
- confirm a gate or delivery action (interactive terminals only — piped or
  scripted runs proceed without prompting);
- enter quick-work details, including an acceptance-criteria loop;
- declare change impact;
- enter local user and workspace details during initialization;
- choose whether to seed a local deployment.

Useful global flags:

| Flag | Behavior |
|---|---|
| `--yes` | Accept confirmations |
| `--no-input` | Disable prompts and fail when input is missing |
| `--plain` | Disable color, Unicode symbols, progress styling, and interactive presentation |
| `--json` | Emit one JSON envelope; implies `--plain --no-input` |
| `--timeout` | Override the default three-minute request timeout |

## Connect to a SpecGate deployment

```bash
specgate config set server http://localhost:8080
specgate doctor
```

Server selection order:

1. `--server`
2. `SPECGATE_SERVER`
3. saved CLI configuration
4. `http://localhost:8080`

Open the human UI:

```bash
specgate open
```

## Choose user and workspace

Interactive `specgate init` creates or reuses a local user and workspace, then
stores the current selection in CLI config. For day-to-day use:

```bash
specgate user current
specgate user list
specgate workspace current
specgate workspace list
specgate workspace select
specgate workspace select <slug>
```

This is an attribution and selection surface. It does not add login, sessions,
or authorization checks.

`workspace select` opens a picker when input is enabled. Use the workspace slug
for non-interactive scripts; internal workspace IDs are not part of the human CLI
surface.

The selected user/workspace is attached to new quick work items as
`created_by` and `workspace_id`. Archive operations use the selected username as
the archive actor. Artifact package publication is lower-level: the package JSON
must still carry its own `created_by` value because packages may be published by
governance services, IDE agents, or other authoring tools.

## Find work

Start with the board overview:

```bash
specgate status
specgate work list
```

Both commands use the selected workspace by default. Use
`specgate status --all-workspaces` or `specgate work list --all-workspaces` when
you intentionally want the global view.

`status` is the board overview: it shows the selected scope, phase counts, how
many items are ready, how many need attention, and a suggested next command.
`work list` is the action queue: it prints exactly the board's needs-attention
section, listing only items that currently need human or agent attention. When
the queue is empty but other work exists, it says which phases still contain
work so you know the board is not empty.

Resolve a change-request ID, SpecGate key, tracker key, or supported issue URL:

```bash
specgate work show "$WORK_REF"
```

Many commands accept no reference in interactive mode and let you choose from
work needing attention.

## Create a quick work item

For a small, understood change, pass the title and criteria directly:

```bash
specgate work create-quick "Fix Redis-free quickstart wording" \
  --description "Redis should be optional when QUEUE_DRIVER=sync." \
  --ac "The release compose starts without a redis service." \
  --ac "Docs state Redis is optional for the sync queue path."
```

Or run it without arguments: the CLI asks for a title, an optional
description, and acceptance criteria one at a time (empty input finishes the
list). Automation can also provide a JSON body. Include `acceptance_criteria`
when the agent or human already has testable criteria; otherwise SpecGate
drafts or falls back to a generic one.

```bash
specgate work create-quick --file work-item.json --json
```

```json
{
  "title": "Fix Redis-free quickstart wording",
  "description": "The quickstart should make Redis optional when QUEUE_DRIVER=sync.",
  "acceptance_criteria": [
    "The release compose starts without a redis service.",
    "Docs state Redis is optional for the sync queue path."
  ]
}
```

The quick route reduces planning ceremony. It does not skip governed context or
delivery review.

## Inspect approved context

```bash
specgate work policy "$WORK_REF"
specgate gates status "$WORK_REF"
specgate work context "$WORK_REF"
```

Fetch selected artifact files when the Context Pack summary is not enough:

```bash
specgate artifact show "$ARTIFACT_ID"
specgate artifact files "$ARTIFACT_ID" spec.md tasks_fe.md
```

`artifact files` returns file references by default. Add `--content` only for the
specific file body you need.

## Publish and check an artifact

Artifact publication accepts a JSON package:

```bash
specgate artifact publish --file artifact.json
```

The package file (`artifact.json` above) uses the IDE/CLI publish shape, not
the lower-level registry `feature_id` + `version` shape. The simplest form points at local files; the
CLI reads them and uploads raw UTF-8 text, so IDE agents do not need to inline
or base64-encode document bodies:

```json
{
  "feature_key": "checkout-loyalty-points",
  "documents": [
    {
      "path": "spec.md",
      "role": "spec",
      "source_file": "spec.md"
    }
  ]
}
```

`source_file` is resolved relative to the package file. Absolute local
`file://` URLs are also accepted as `file_url`, but Doc Registry never stores or
dereferences local file URLs; the CLI converts them to raw content before
upload.

In interactive mode, the CLI collects an impact declaration when the package
does not include one.

Run artifact-scoped quality/readiness checks:

```bash
specgate gates check "$ARTIFACT_ID"
```

Readiness is not approval. Review and approval remain separate governed actions.

## Run quality gates

```bash
specgate gates run "$WORK_REF"
specgate gates status "$WORK_REF"
specgate gates history "$WORK_REF"
```

`gates run` confirms only in interactive terminals; non-interactive runs
(`--json`, piped stdin) proceed without prompting, so `--yes` is optional
there:

```bash
specgate gates run "$WORK_REF" --json
```

## Report and review delivery

The primary path is two commands. Scaffold a completion report with one
`criteria[]` entry per acceptance criterion:

```bash
specgate delivery report "$WORK_REF" --init
```

Fill in the summary, affected files, checks, and per-criterion claims, then
submit the whole delivery tail — report, gates, delivery review, and the
per-criterion verdict — in one command:

```bash
specgate delivery submit "$WORK_REF" --file completion.json
```

The individual commands remain available when you need one stage at a time:

```bash
specgate delivery report "$WORK_REF" --file completion.json --json
specgate delivery review "$WORK_REF"
specgate delivery status "$WORK_REF" --detail
```

`delivery report` without `--file` records a minimal interactive completion
event. If review fails, use the outstanding findings to make a focused
correction, report completion again, and repeat review (or rerun
`delivery submit`).

## See whether governance is helping

`specgate stats` projects a governance-value readout from existing gate runs
and feedback events — reviewed items, first-pass yield, catches before and
after the build, rework, ambiguity saves, cycle time, and a ledger of recent
catches:

```bash
specgate stats                    # selected workspace, last 30 days
specgate stats --days 90 --all-workspaces
```

Until a few governed work items have been through delivery review, it prints
an honest "not enough data yet" line instead of empty percentages.

## Advanced policy and gate-task tools

For a work item, use the core workflow policy command:

```bash
specgate work policy "$WORK_REF"
```

Inspect built-in governance levels:

```bash
specgate policy list
```

Inspect pending IDE-agent gate tasks:

```bash
specgate gates tasks list "$ARTIFACT_ID"
specgate gates tasks show "$TASK_ID"
```

## Use the CLI in automation

`--json` emits one stable envelope:

```json
{
  "schema_version": "specgate.cli/v1",
  "command": "status",
  "ok": true,
  "data": {}
}
```

Errors use:

```json
{
  "schema_version": "specgate.cli/v1",
  "command": "doctor",
  "ok": false,
  "error": {
    "code": "unavailable",
    "message": "connection failed",
    "details": {}
  }
}
```

Example:

```bash
specgate --json status
```

Use `--json-progress` for compact progress events before a long command’s final
JSON envelope:

```bash
specgate --json --json-progress update
```

## Update and diagnose the CLI

```bash
specgate update
specgate doctor
```

`update` refreshes the CLI from GitHub, refreshes installed IDE setup for Codex,
Claude Code, and Cursor from the public plugin registry, then updates the local
Compose bundle and images when a CLI-managed deployment exists. That Compose
step pulls whatever services the release bundle ships, including Doc Registry,
agents, and the web UI. Restart any running IDEs after an update.

Released CLI builds also check GitHub releases for public freshness during
human/plain server-backed commands. The check is cached for 24 hours and only
prints a warning; use `SPECGATE_NO_UPDATE_CHECK=1` to disable it in local
automation that is not already using `--json` or `CI=true`.

Use `specgate doctor` after updating to confirm the CLI is connected to the
intended deployment; when a CLI-managed deployment exists, `doctor` also shows
a "Local stack" section with the running compose services (`local-status`
serves the same data for scripts). If your local stack is on a non-default port,
run `specgate config set server http://localhost:<port>` so future
server-backed workflow commands target the right stack.

For current command arguments:

```bash
specgate <command> --help
```

See [CLI reference](../reference/cli.md) for command families and exit codes.
