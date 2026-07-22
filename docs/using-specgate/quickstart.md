# Quickstart

This tutorial takes the default Local CLI path through one governed work item.
Your IDE agent prepares the specification and delivery evidence. You keep the
two decisions that matter: approving the exact artifact version and approving
the final delivery.

## Before you start

- macOS, Linux, or Windows with WSL2.
- Network access for installation.
- A repository with an IDE agent you can restart.

You do not need Docker, a source checkout of SpecGate, or a model API key. After
installation, Local mode works without a server or network connection.

> **Windows:** run the install script and CLI from WSL2. Native Windows binaries
> are also available on the GitHub releases page.

## 1. Install

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate --version
```

If `specgate` is not found, add the install directory printed by the installer
to your `PATH`. The default is usually `~/.local/bin`.

## 2. Initialize Local mode

From your repository:

```bash
specgate init
```

Choose **Local CLI**, the default. The prompts create a local user and workspace
and print the plugin-install command. Bind this repository to that workspace:

```bash
specgate workspace bind
```

Local state uses SQLite on your machine; no Docker container, browser, or TCP
service starts.

| Capability | Local CLI (default) | Full appliance |
| --- | --- | --- |
| Best for | One person using a terminal and IDE agent | Teams and browser-based workflows |
| Runtime | CLI only; no Docker, server, browser, or TCP service | One local Docker appliance container |
| Durable state | SQLite on this machine | Appliance-managed database and object storage; Governance-chat threads reset when the appliance restarts |
| Core workflow | Immutable artifacts, readiness tasks, Context Packs, delivery evidence, and human decisions | Everything in Local, backed by shared services and a browser UI |
| Additional capabilities | Local users, workspaces, and Codex, Claude Code, or Cursor plugins | Governance chat, Knowledge, integrations, model-backed checks, and shared workspaces |

For automated Local setup:

```bash
specgate init --mode local --no-input \
  --workspace-name "My workspace" \
  --display-name "Jane Doe" \
  --username jane
```

Email is optional: add `--email jane@example.com` when needed.

## 3. Install the IDE plugin

For Codex:

```bash
specgate plugins install --agent codex --project-local
specgate plugins doctor --agent codex --project-local
```

Restart the IDE agent so it loads the installed skills and rules. Claude Code
and Cursor use the same commands with `--agent claude` or `--agent cursor`; see
[Install IDE plugins](guides/install-ide-plugins.md).

Check the Local setup:

```bash
specgate doctor
specgate status
```

## 4. Ask the agent to prepare the work

Give your IDE agent a bounded request with acceptance criteria. For example:

> Use SpecGate to prepare a governed change that adds a `/healthz` endpoint.
> It must return 200 while the service is healthy and have an automated test.
> Show me the artifact preview before publishing anything.

The installed plugin tells the agent how to inspect the repository, prepare an
artifact package, and pause for your confirmation. Your IDE agent prepares and
publishes through this sequence:

```bash
specgate artifact publish --file artifact.json --preview --json
# The agent shows the preview and waits for your confirmation.
specgate artifact publish --file artifact.json --json
specgate gates check <artifact-id> --json --summary
specgate gates tasks list <artifact-id> --json
specgate gates tasks show <task-id> --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
specgate gates results <artifact-id> --json
```

`gates check` may report `aggregate=not_run` while semantic IDE tasks are still
pending. That is not a readiness pass. The agent must submit the frozen tasks
and read the final gate results.

## 5. Approve the artifact

Inspect what was stored:

```bash
specgate artifact show <artifact-id> --json
specgate gates results <artifact-id> --json
```

You approve the exact artifact version and the explicit implementation handoff
as the human user with one resumable decision:

```bash
specgate --yes change approve <artifact-id> \
  --title "Add healthcheck endpoint" \
  --ac "GET /healthz returns 200 while the service is healthy" \
  --ac "The endpoint has an automated test @check:tests" \
  --json
```

Approval is separate from readiness. If the artifact is wrong, ask the agent to
publish a corrected immutable version instead of editing the approved body.

The receipt returns `<work-ref>` for work bound to the canonical version and
confirms its Context Pack is assembled. Tell the agent to continue by reading
that exact handoff:

```bash
specgate work context <work-ref> --json
```

The Context Pack identifies the approved artifact version. The agent should
stop if it cannot retrieve that context.

## 6. Let the agent implement and submit evidence

The IDE agent implements only the bounded Context Pack, runs the repository
checks, and prepares one evidence claim per acceptance criterion:

```bash
specgate delivery report <work-ref> --init --json
COMPLETION_PATH="<exact data.path from the preceding response>"
specgate change submit <work-ref> \
  --file "$COMPLETION_PATH" \
  --run-checks --yes --json
specgate change status <work-ref>
```

Use the scaffold command's exact `data.path` value in `--file`; do not build a
filename from a URL or another arbitrary work reference.

If review reports a gap, the agent fixes that gap, reruns the relevant checks,
updates the evidence, and submits again. A passing readiness check or agent
review is evidence; neither is human approval. Use
`specgate delivery status <work-ref> --detail` only when you need the expert
evidence breakdown.

## 7. Make the final decision

Review the submitted files, checks, criterion evidence, and verdict:

```bash
specgate change status <work-ref>
specgate verify <work-ref>
```

You make the final delivery decision:

```bash
specgate --yes change accept <work-ref>
```

Use `specgate --yes change request-changes <work-ref>` instead if the evidence
or result is not acceptable. Then inspect the durable trail:

```bash
specgate audit <work-ref> --json
specgate stats --json
```

Restarting your terminal or IDE does not remove Local artifacts, approvals, work
items, evidence, or audit history.

## Full appliance

Choose Full when you need the browser UI, Governance chat, Knowledge,
integrations, model-backed platform checks, or shared server-backed workspaces:

```bash
specgate init --mode full
```

For the Full appliance workflow, see [Operate SpecGate](guides/operate-specgate.md).
Model setup is covered in [Configure models](guides/configure-models.md).

## Safe removal

`specgate uninstall` removes CLI configuration and globally installed managed
plugin files but keeps Local SQLite state:

```bash
specgate uninstall
```

To delete SpecGate's Local SQLite state too:

```bash
specgate uninstall --purge-data --yes
```

The purge warns first and preserves repositories, project specifications, and
unrelated files in the selected directory. It does not start or require Docker.
Project-local plugin files in this repository are preserved; review and remove
them through normal repository file cleanup when you no longer want them.

## Troubleshooting

Start with:

```bash
specgate doctor
specgate status
```

Errors include a recovery command where SpecGate can determine one. For plugin
problems, rerun:

```bash
specgate plugins doctor --agent codex --project-local
```

For Full appliance health, use `specgate local-status` and the
[operations guide](guides/operate-specgate.md).

## Next steps

- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [How verification works](concepts/verification.md)
- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md)
- [CLI reference](reference/cli.md)
