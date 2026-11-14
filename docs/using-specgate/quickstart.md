# Quickstart

Complete one Local work item with your coding agent and the CLI. You approve the
scope, the agent submits evidence, and you accept the delivery or request
changes. The whole loop runs through the CLI.

## 1. Set up the CLI and coding agent

On macOS, Linux, or WSL2, install the CLI:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
```

On native Windows, extract `specgate.exe` from `windows_amd64.zip` in the
[latest release](https://github.com/thanhtung2693/specgate/releases/latest) to a
folder on `PATH`. Install a POSIX `sh` on `PATH` to rerun delivery checks.
If the shell cannot find `specgate`, add the installer's
printed directory to `PATH` (typically `~/.local/bin` on macOS and Linux).

From the Git repository root, run:

```bash
specgate init --mode local
specgate doctor
```

Setup creates a Local user and workspace and binds the repository. If you
skipped IDE integration during setup, install it now. For Codex:

```bash
specgate plugins install --agent codex --project-local
specgate plugins doctor --agent codex --project-local
```

Restart the coding agent so it loads the skills. See [Install IDE plugins](guides/install-ide-plugins.md)
for Claude Code, Cursor and other installation options. Local works with SQLite
on this machine and needs no Docker, server, or model API key. For scripted
setup, see [Configuration](reference/configuration.md#cli).

## 2. Prepare and approve the scope

Ask your coding agent for a small, testable change:

> Use SpecGate to prepare a change that adds a healthcheck. It must return
> HTTP 200 at `/healthz` and have an automated test. Show me the artifact
> preview before publishing. Stop for approval before implementing.

The agent prepares the specification, previews and publishes the exact version
with your confirmation, and completes the readiness tasks. Review that version
and its criteria. The agent can supply the approval command, or you can run:

```bash
specgate --yes change approve <artifact-id> \
  --title "Add healthcheck endpoint" \
  --ac "GET /healthz returns HTTP 200" \
  --ac "An automated test checks the healthcheck response @check:tests"
```

Readiness results inform your approval. Use the work reference from the approval
receipt for the rest of the loop. See [Use the SpecGate CLI](guides/cli-workflow.md)
for artifact and gate commands or the smaller quick-work route.

## 3. Let the agent implement and submit evidence

Give the agent the exact reference. For example, “Implement SpecGate work
`LOCAL-123`. Submit evidence and stop when a human decision is needed.” The
agent starts with:

```bash
specgate work resume LOCAL-123 --json
```

The agent reads the approved scope and criteria, implements the change, runs
checks, and prepares one evidence claim per criterion. It initializes the
report with `specgate delivery report LOCAL-123 --init --json` and passes the
returned report path to `specgate change submit`. Review the check commands
before authorizing `--run-checks`, which runs them as your OS user. Ask for
the receipt with the work reference, criterion evidence, and exact next command. See
[Use SpecGate with a coding agent](guides/coding-agent-workflow.md) for the full
agent sequence.

## 4. Review the evidence and decide

Read the receipt and the current work status. Use `--json` for structured
criterion evidence and the decision identifiers.

```bash
specgate change status LOCAL-123
```

When status shows `awaiting_acceptance`, copy its exact next command to accept
a delivery you have reviewed. The default form is
`specgate --yes change accept LOCAL-123 --review-id <review-id>`. To request
changes, use the review ID from that same status:

```bash
specgate --yes change request-changes LOCAL-123 --review-id <review-id> --note "<what needs fixing>"
```

If the status includes a basis digest, include its exact value and any selected
comparison flags from the next command in your decision. After a request for
changes, give the agent the same work reference and review its new submission
before deciding again.

## Optional browser review

With a build that includes Local browser review, run `specgate ui LOCAL-123`
to inspect a handoff. Stop the foreground command to close the session, then
continue with `specgate change status LOCAL-123` in the same repository.
See [Local review](guides/local-review.md) and the [changelog](../../CHANGELOG.md).

## Where to go next

- [Advanced Local verification](reference/cli.md#advanced-local) covers optional
  verification pins, JUnit observations, checkpoints, and lineage.
- [Try a delivery evidence gap](guides/try-delivery-proof.md) walks through
  a skipped required check, an agent repair, and a fresh review.
- [Local for small teams](concepts/who-specgate-is-for.md#for-teams-names-on-decisions)
  explains Git/PR collaboration with per-machine stores.
- [Try Local review notifications](guides/local-notification-pilot.md) describes
  the optional foreground pilot from the source checkout. The participant study
  is pending.
- [Operate SpecGate](guides/operate-specgate.md) covers backups and recovery for previous installations.
- [Local CLI uninstall](reference/cli.md#local-cli-uninstall) explains
  how to remove the CLI and when Local state is kept.
- [CLI reference](reference/cli.md) lists the commands by purpose.
