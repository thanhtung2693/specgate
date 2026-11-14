---
name: specgate
description: Use when the working repository is SpecGate-governed, or when the user mentions SpecGate, the specgate CLI, or a SpecGate artifact, gate, Context Pack, work reference, or delivery state.
---

# Using SpecGate

## Bootstrap-only install

Use only when a root-only skills.sh install lacks `specgate-project-setup`.
Check `command -v specgate` (PowerShell: `Get-Command specgate`). If absent,
explain skills.sh installs instructions only. On macOS, Linux, or WSL2, show
this and await approval:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

Native Windows: use `https://github.com/thanhtung2693/specgate/releases/latest`;
never invent PowerShell. Record `specgate --version`.

Trust only a `thanhtung2693/specgate` lock for
`plugins/skills/specgate/SKILL.md` in project `skills-lock.json` or global
`~/.agents/.skill-lock.json`. Show matching removal and await approval:

```bash
npx skills remove specgate -y
npx skills remove specgate -g -y
```

Never edit skills.sh files or locks. Ask IDE and scope, then preview, install,
and verify:

```bash
specgate plugins install --agent <codex|claude|cursor> [--project-local] --dry-run --no-input
specgate plugins install --agent <codex|claude|cursor> [--project-local] --no-input
specgate plugins doctor --agent <codex|claude|cursor> [--project-local] --json
```

On native plugin ownership, stop; never uninstall or disable it without
separate approval. Require IDE restart before `specgate-project-setup`.

## Route one phase

For lifecycle work, choose exactly one phase before acting:

- `specgate-project-setup` — configure project or plugins.
- `specgate-work-preparation` — request/spec to approved work.
- `specgate-work-delivery` — implement, resume, review, or rework approved work.

For a read-only work or lifecycle-status question with a work reference, start
with the authoritative read:

```bash
specgate change status "$WORK_REF" --json
```

Without a work reference, discover it once:

```bash
specgate work list --phase ready --json
```

Choose one `next_actor=implementing_agent` row. Ready may await a human.
None or several: ask the human.

For other concept or troubleshooting questions, use the smallest relevant CLI
read. Do not force a phase or mutate records.

Before the first workspace operation, run `specgate doctor --json`. Follow its
reported recovery on failure. Reuse the result until configuration, workspace,
or repository changes, or a command reports a setup error.

## Operating contract

- The `specgate` CLI is the only product-state read and write surface. Never inspect
  or edit SpecGate SQLite or `.specgate/local` directly. Repository source
  reads remain allowed.
- Drafts and repository reads stay ephemeral until a CLI write persists them.
- The originating authoring framework owns durable source documents: their
  paths, names, lifecycle, and Git policy. SpecGate snapshots them in place. It
  does not move, copy, rename, delete, commit, or change ignore rules for them.
- A readiness pass is not human approval. Approval, acceptance, and requested
  changes remain human decisions. Run a decision command only after the human
  explicitly chooses and authorizes that exact decision; never infer one.
- An approved Context Pack outranks chat history, tracker prose, and stale
  repository documentation. Never silently expand its scope.
- Follow exact IDs and versions; `artifact coverage <artifact-id>` is exact-version evidence.
- Follow `change status.data` (or Local `work resume.data.status`) fields
  `next_actor` and `next_command`. When the next actor
  is human, stop and hand off that command verbatim.
- For human handoffs or requested views, use `specgate-human-handoff`.
  Local browser review is opt-in. Only use CLI-returned URLs.

For command syntax, run `specgate <command> --help` rather than reconstructing
flags from memory.
