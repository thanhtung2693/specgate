# Quickstart

Use Quick Work to finish one change with your coding agent and the Local CLI.
It is useful when a requirement could slip through, a regression needs a test,
or another session needs to pick up the work. For a typo or a label change,
your usual agent and tests are enough.

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

## 2. Agree on criteria and create Quick Work

Pick a change with a clear outcome. In the example below, trimming already has
a test, but there is no check for a name that becomes empty after trimming.
Ask your coding agent:

> Prepare a Quick Work item to normalize names. It must trim surrounding
> whitespace and reject names that are empty after trimming. Show me the two
> criteria and the proposed test commands. Wait for me to approve them.

After you approve, the agent creates the work item. For a JavaScript project
with these test filenames, the command is:

```bash
specgate work create-quick "Normalize user names" \
  --description "Trim surrounding whitespace and reject empty trimmed names." \
  --ac "Trim surrounding whitespace @check:trim" \
  --ac "Reject a name that is empty after trimming @check:reject-empty"
```

The `@check` labels connect each criterion to a named check in the agent's
report. In this example, review `node --test trim.test.mjs` and
`node --test reject-empty.test.mjs` before authorizing them. For your own
project, ask your agent to propose the actual commands and adjust the criteria
with you first. Quick Work gives the agent a Context Pack from those criteria
without publishing an artifact. If the work needs a frozen version of a reviewed
spec or plan, use [artifact-backed work](guides/cli-workflow.md#publish-artifact-backed-work).

## 3. Let the agent implement and submit evidence

Give the agent the work reference returned by the CLI:

> Implement this SpecGate work. Submit evidence for each criterion and stop
> when it's my turn to review.

The agent runs `specgate work resume <work-ref> --json`, reads the Context
Pack, implements the change, and starts a report with
`specgate delivery report <work-ref> --init --json`. It fills the returned
file and submits it using the returned path. After you approve the check
commands, the agent can use the following form. `--run-checks` runs those
commands as your OS user.

```bash
specgate --yes change submit "$work_ref" --file "$report_path" --run-checks
```

If the trim test passes and the empty-name check was skipped, the latter
criterion still lacks evidence. Give the agent the same work reference so it
can add the missing behavior and test, rerun the checks, and submit again.
If you want to try this on disposable data, follow the
[Local delivery proof](guides/try-delivery-proof.md).

## 4. Review the evidence and decide

Read the receipt and the current work status. A CLI-observed pass tells you
the recorded command succeeded. It cannot tell you whether the test checks the
right behavior or whether the code is sound. Inspect the checks and decide
whether to ask for stronger evidence. Use `--json` for structured results
and exact decision identifiers.

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
- [Move a Local store](guides/operate-specgate.md#move-a-local-store-to-another-checkout)
  shows how to continue the same work on another machine.
- [Local CLI uninstall](reference/cli.md#local-cli-uninstall) explains
  how to remove the CLI and when Local state is kept.
- [CLI reference](reference/cli.md) lists the commands by purpose.
