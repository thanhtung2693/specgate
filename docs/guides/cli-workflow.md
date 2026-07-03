# Use the SpecGate CLI

Use this guide when you need to inspect governed work, publish artifacts, hand
context to a coding agent, or submit delivery evidence from a terminal.

For exact flags and JSON shapes, see the [CLI reference](../reference/cli.md).

## Choose an output mode

Default output is for humans: color, compact summaries, and prompts.

Use plain text for predictable terminal logs:

```bash
specgate --plain status
```

Use JSON for automation and IDE tools:

```bash
specgate --json status
```

`--json` implies `--plain --no-input`. Automation should pass every required
value explicitly.

## Connect to a deployment

Set the saved server URL:

```bash
specgate config set server http://localhost:8080
specgate doctor
```

Server selection order:

1. `--server <url>`
2. `SPECGATE_SERVER`
3. saved CLI config
4. `http://localhost:8080`

Open the web UI:

```bash
specgate open                    # base URL
specgate open <work-ref>         # work item page
specgate open reviews            # section page: reviews, artifacts, or work
specgate open --artifact <id>    # artifact inspector
```

## Select user and workspace

Interactive `specgate init` creates or reuses a local user and workspace. Check
the current selection:

```bash
specgate user current
specgate workspace current
```

Select a different workspace:

```bash
specgate workspace list
specgate workspace select
specgate workspace select <slug>
```

User/workspace selection is attribution and filtering, not authentication. New
quick work items receive the selected `created_by` and `workspace_id`.

## Find work that needs attention

```bash
specgate status
specgate work list
```

Both commands use the selected workspace by default. Use the global view only
when you mean it:

```bash
specgate status --all-workspaces
specgate work list --all-workspaces
```

Inspect one work item:

```bash
specgate work show <work-ref>
specgate work policy <work-ref>
```

`<work-ref>` can be a change-request ID, SpecGate key, tracker key, or supported
issue URL.

## Create a quick work item

Use the quick route for small, understood work:

```bash
specgate work create-quick "Fix Redis-free quickstart wording" \
  --ac "The release compose starts without Redis when QUEUE_DRIVER=sync" \
  --ac "Docs state Redis is optional for the sync queue path"
```

If you omit `--description`, the title is used as the description. Run the
command without arguments for interactive prompts.

Automation can send a JSON body:

```bash
specgate work create-quick --file work-item.json --json
```

```json
{
  "title": "Fix Redis-free quickstart wording",
  "description": "Redis should be optional when QUEUE_DRIVER=sync.",
  "acceptance_criteria": [
    "The release compose starts without Redis when QUEUE_DRIVER=sync.",
    "Docs state Redis is optional for the sync queue path."
  ]
}
```

The quick route reduces planning ceremony. It still creates governed context and
delivery review.

## Read approved context

Before implementation:

```bash
specgate work policy <work-ref>
specgate gates status <work-ref>
specgate work context <work-ref>
```

Fetch artifact files only when the Context Pack is not enough:

```bash
specgate artifact show <artifact-id>
specgate artifact files <artifact-id> spec.md verification.md
specgate artifact files <artifact-id> spec.md --content
```

Use `--content` sparingly. It prints file bodies.

## Publish an artifact package

Create a package JSON that points at local files:

```json
{
  "feature_key": "checkout-loyalty-points",
  "documents": [
    {
      "path": "spec.md",
      "role": "spec",
      "source_file": "spec.md"
    },
    {
      "path": "verification.md",
      "role": "verification",
      "source_file": "verification.md"
    }
  ]
}
```

Publish it:

```bash
specgate artifact publish --file artifact.json
```

The CLI reads `source_file` content and uploads raw UTF-8 text. Doc Registry
does not dereference local file URLs itself.

Run readiness checks:

```bash
specgate gates check <artifact-id>
```

Readiness is not approval. Approval is a separate governed action.

## Review and approve from the CLI

Approve an artifact version, or send it back with a note:

```bash
specgate artifact approve <artifact-id> --note "LGTM"
specgate artifact request-changes <artifact-id> --note "Tighten the error copy"
```

Both record the selected user as the deciding actor. Interactive terminals ask
for confirmation first; `--json` and non-interactive runs proceed directly.

Review pending artifact-update proposals (agent-drafted edit sessions awaiting
a human decision):

```bash
specgate artifact proposals
specgate artifact proposals approve <session-id>
specgate artifact proposals reject <session-id>
```

`approve` saves the proposal as a draft revision; `reject` discards it —
interactive terminals confirm the discard, `--yes` skips the prompt.

`artifact show` also accepts a unique id prefix from the `artifact list` table:

```bash
specgate artifact list
specgate artifact show <id-prefix>
```

## Run gates for work

```bash
specgate gates run <work-ref>
specgate gates status <work-ref>
specgate gates history <work-ref>
```

Interactive terminals ask before running gates. JSON and non-interactive runs
proceed without prompting:

```bash
specgate gates run <work-ref> --json
```

## Submit delivery evidence

Scaffold the completion report:

```bash
specgate delivery report <work-ref> --init
```

Fill `completion.json` with:

- summary;
- affected files;
- checks;
- per-criterion claims and evidence.

Submit the whole delivery tail:

```bash
specgate delivery submit <work-ref> --file completion.json
specgate delivery status <work-ref> --detail
```

`delivery submit` reports completion, runs gates, triggers delivery review, and
prints the resulting status. If review fails, fix the named gap, update
evidence, and submit again.

## Measure governance value

```bash
specgate stats
specgate stats --days 90 --all-workspaces
```

`stats` reports reviewed items, first-pass yield, catches before and after
implementation, rework, ambiguity saves, cycle time, and recent catches. It is
most useful after several work items have completed delivery review.

## Use the CLI in automation

Rules for scripts:

- use `--json --no-input`;
- pass all required values explicitly;
- set `--server` or `SPECGATE_SERVER`;
- store command output as evidence when useful;
- do not parse human output.

Example:

```bash
specgate --json --no-input --server "$SPECGATE_SERVER" \
  delivery submit "$WORK_REF" --file completion.json
```

## Update or diagnose the CLI

```bash
specgate version
specgate doctor
specgate update
```

`update` refreshes the CLI, IDE plugin files, and a CLI-managed local stack when
one exists.

## Related

- [Quickstart](../quickstart.md)
- [CLI reference](../reference/cli.md)
- [Use SpecGate with a coding agent](coding-agent-workflow.md)
