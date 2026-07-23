# Use the SpecGate CLI

Use this guide for the normal SpecGate loop: find governed work, give an IDE
agent the approved context, submit implementation evidence, and make the human
delivery decision.

For every command and flag, see the [CLI reference](../reference/cli.md).

## Choose output for the caller

The default output is interactive and designed for people.

```bash
specgate status
```

Use plain output for stable terminal logs:

```bash
specgate --plain status
```

Use JSON for IDE agents and automation:

```bash
specgate --json status
```

JSON mode is non-interactive. Pass required values explicitly and read results
from stdout; progress and warnings use stderr.

## Confirm your workspace

Local mode creates a user and workspace during `specgate init`. Full mode can
also connect to an existing SpecGate server.

Check the current identity and workspace:

```bash
specgate user current
specgate workspace current
```

Select or bind a workspace when needed:

```bash
specgate workspace list
specgate workspace select <slug>
specgate workspace bind <slug>
```

`workspace bind` associates the current Git checkout with that workspace
without writing a project file. Use a one-command override to inspect another
workspace:

```bash
specgate --workspace platform status
```

Use `--all-workspaces` only when you intentionally want a global view.

For Full mode, save and verify the server:

```bash
specgate config server http://localhost:3000/api/doc-registry
specgate doctor
```

## Complete an existing change

### 1. Find work

```bash
specgate status
specgate work list
specgate work show <work-ref>
```

`<work-ref>` can be a SpecGate key, change-request ID, supported tracker key,
or supported issue URL.

### 2. Read the governing context

Before implementation, read the policy, gate state, and Context Pack:

```bash
specgate work policy <work-ref>
specgate gates status <work-ref>
specgate work context <work-ref>
```

The Context Pack is the implementation contract for the selected work. It
keeps the approved artifact version, acceptance criteria, applicable skills,
and current governance state together.

### 3. Check the next action

```bash
specgate change status <work-ref>
```

Status separates:

- implementation evidence;
- independent assurance;
- the human decision;
- the recorded Git receipt.

Follow the exact next command shown. A passing automated review is evidence,
not human acceptance.

### 4. Prepare completion evidence

After implementation, scaffold a completion report:

```bash
specgate delivery report <work-ref> --init
```

Fill in the coding agent name, summary, affected files, checks, and evidence for
each acceptance criterion. The generated file is intentionally incomplete so
an agent cannot claim delivery without adding evidence.

For machine-readable setup:

```bash
specgate delivery report <work-ref> --init --json
COMPLETION_PATH="<exact data.path from the preceding response>"
```

Use the returned `data.path`; do not guess the filename.

### 5. Submit and review

Submit the default report:

```bash
specgate change submit <work-ref>
```

Or submit an explicit path:

```bash
specgate change submit <work-ref> --file "$COMPLETION_PATH"
```

To rerun the report's non-skipped checks before submission:

```bash
specgate change submit <work-ref> --file <completion.json> --run-checks
```

Review status again:

```bash
specgate change status <work-ref>
```

If it reports `State: review_pending`, run the exact
`specgate delivery review <work-ref>` command shown, then check status again. If
it reports `State: awaiting_review`, inspect the detailed readback:

```bash
specgate delivery status <work-ref> --detail
```

### 6. Make the human decision

Run exactly one of the following human decisions when status reports
`State: awaiting_acceptance`. Accept only after reviewing the evidence and
implementation:

```bash
# Local
specgate --yes change accept <work-ref> --note "Approved after review"

# Full
specgate change accept <work-ref> --note "Approved after review"
```

If status is not ready for acceptance, request another implementation cycle:

```bash
# Local
specgate --yes change request-changes <work-ref> \
  --note "Please address the failing check"

# Full
specgate change request-changes <work-ref> \
  --note "Please address the failing check"
```

Local mode requires `--yes` for either decision. Full mode confirms
interactively when possible. Do not issue both decisions for the same
completion.

### 7. Verify closeout

```bash
specgate verify <work-ref> --json
```

This read-only command reports the governing artifact version, every acceptance
criterion, checks, delivery verdict, cleanup eligibility, and next command. It
exits with a nonzero status while the work is not closeable.

## Create small, understood work

Use the quick route when the work does not need an artifact package:

```bash
specgate work create-quick "Correct quickstart wording" \
  --ac "The installation command is accurate" \
  --ac "The documented verification succeeds"
```

Local mode requires explicit acceptance criteria. An IDE agent using the
SpecGate preparation skill can draft criteria, show them for confirmation, and
then create the work item in either mode.

Quick work still gets a Context Pack and the complete delivery-review loop.

## Publish artifact-backed work

For larger work, preview an explicitly mapped artifact package:

```bash
specgate artifact publish --file artifact.json --preview --json
```

After confirming the preview, publish it:

```bash
specgate artifact publish --file artifact.json
```

Run readiness:

```bash
specgate gates check <artifact-id> --json --summary
```

Approve and create the implementation handoff:

```bash
specgate --yes change approve <artifact-id> \
  --title "<work title>" \
  --ac "<confirmed criterion>"
```

Then resume the normal delivery loop with the returned work reference.

SpecGate does not require a particular specification framework or directory.
Your framework keeps its files where it normally creates them; the publish
package maps those files into a versioned artifact.

## Check coverage and outcomes

Find published specifications without corresponding delivered work:

```bash
specgate coverage
```

Review governance signals after several work items:

```bash
specgate stats
```

These are workflow signals for human interpretation, not proof that SpecGate
prevented a defect.

## Move Local history into Full mode

Export a Local workspace, switch to Full mode, select the destination
workspace, and inspect the dry run before importing:

```bash
specgate portable export --file ~/specgate-local.json
specgate portable import --file ~/specgate-local.json --dry-run
specgate portable import --file ~/specgate-local.json --yes
```

Portable import is a one-way move, not synchronization. It does not transfer
credentials or merge conflicting records.

## Diagnose and update

```bash
specgate version
specgate doctor
specgate update
```

In Local mode, `doctor` checks the local store, identity, workspace, and IDE
setup. In Full mode, it also checks server and appliance health.

`update` refreshes the CLI, already-installed global IDE plugin files, and a
CLI-managed Full appliance when present. Project-local plugin files are scoped
to each repository; refresh them there:

```bash
specgate plugins install --project-local
```

## Related

- [Quickstart](../quickstart.md)
- [CLI reference](../reference/cli.md)
- [Use SpecGate with a coding agent](coding-agent-workflow.md)
- [Respond to gate failures](respond-to-gate-failures.md)
