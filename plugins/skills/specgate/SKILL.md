---
name: specgate
description: Use when the user explicitly mentions SpecGate, the specgate CLI, or a SpecGate artifact, gate, Context Pack, work reference, or delivery state.
---

# Using SpecGate

## Bootstrap-only install

Use when `specgate-project-setup` is unavailable after a root-only skills.sh
install. Check `command -v specgate` (PowerShell: `Get-Command specgate`). If
absent, explain skills.sh provides instructions only. On macOS, Linux, or
WSL2, show this and await explicit approval:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

Native Windows: use `https://github.com/thanhtung2693/specgate/releases/latest`;
never invent PowerShell. Record `specgate --version` afterward.

Trust only a lock with source `thanhtung2693/specgate` and path
`plugins/skills/specgate/SKILL.md`, in project `skills-lock.json` or global
`~/.agents/.skill-lock.json`. Show its matching command and await approval:

```bash
npx skills remove specgate -y
npx skills remove specgate -g -y
```

Never edit or delete skills.sh files or locks directly. Ask for IDE and scope,
then preview, install, and verify:

```bash
specgate plugins install --agent <codex|claude|cursor> [--project-local] --dry-run --no-input
specgate plugins install --agent <codex|claude|cursor> [--project-local] --no-input
specgate plugins doctor --agent <codex|claude|cursor> [--project-local] --json
```

On native plugin ownership, stop. Never uninstall or disable it without
separate explicit approval.

Require an IDE restart and stop before initialization. Continue through
`specgate-project-setup` after restart.

Completion: one CLI-managed plugin, healthy doctor, explicit restart.

## Route one phase

For lifecycle work, choose exactly one phase before acting:

- `specgate-project-setup` — initialize, bind, install, refresh, or diagnose SpecGate for a repository.
- `specgate-work-preparation` — turn a request or source specification into approved SpecGate work.
- `specgate-work-delivery` — implement, resume, review, or rework approved SpecGate work.

For a read-only work or lifecycle-status question with a work reference, start
with the authoritative read:

```bash
specgate change status "$WORK_REF" --json
```

For other SpecGate concept or troubleshooting questions, use the smallest
relevant CLI read. Do not force a lifecycle phase or mutate records.

Before a mode-dependent write or handoff, run `specgate doctor --json`. Read
`data.mode`; never infer Local or Full mode from Docker, URLs, or browser
availability. Report an unsuccessful doctor result instead of guessing.

## Operating contract

- The `specgate` CLI is the only product-state read and write surface. Never inspect
  or edit SpecGate SQLite, Postgres, object storage, deployment volumes, or
  `.specgate/local` directly. Repository source reads remain allowed.
- Drafts, explanations, summaries, and repository reads remain ephemeral until
  an explicit CLI command persists them.
- The originating authoring framework owns durable source documents: their
  paths, names, lifecycle, and Git policy. SpecGate snapshots them in place. It
  does not move, copy, rename, delete, commit, or change ignore rules for them.
- A readiness pass is not human approval. Approval, acceptance, and requested
  changes remain human decisions. Run a decision command only after the human
  explicitly chooses and authorizes that exact decision; never infer one.
- An approved Context Pack outranks chat history, tracker prose, and stale
  repository documentation. Never silently expand its scope.
- Follow exact identifiers and versions. `artifact coverage <artifact-id>` is
  exact-version evidence; do not collapse versions by feature name.
- Follow `change status.data.next_actor` and `next_command`. When the next actor
  is human, stop and hand off that command verbatim.
- Local mode has no UI URL and never calls `specgate open`. In Full mode, use
  only the URL returned by `specgate open ... --print --json`; never construct
  one.

For command syntax, run `specgate <command> --help` rather than reconstructing
flags from memory.
