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

`--plain` disables interactive prompts. `--json` implies
`--plain --no-input`. Automation should pass every required value explicitly.

## Connect to a Full deployment

Local CLI mode has no server to configure. The commands in this section apply
when you selected Full appliance or connect to an existing server.

Set the saved server URL:

```bash
specgate config server http://localhost:3000/api/doc-registry
specgate doctor
```

Server selection order:

1. `--server <url>`
2. `SPECGATE_SERVER`
3. saved CLI config
4. `http://localhost:3000/api/doc-registry`

Open the web UI:

```bash
specgate open                    # base URL
specgate open <work-ref>         # work item page
specgate open reviews            # section page: reviews, artifacts, or work
specgate open <work-ref> --print # canonical URL, no browser side effect
specgate open --artifact <id>    # artifact inspector
```

## Select user and workspace

Interactive Local CLI `specgate init` creates or reuses a local user and
workspace. When connecting to an existing server or refreshing Full CLI
identity settings, use
`user login`:

```bash
specgate user login \
  --workspace "My workspace" \
  --display-name "Jane Doe" \
  --username jane \
  --email jane@example.com
```

Clear the local user plus global/project workspace selections without deleting
server data:

```bash
specgate user logout
```

Check the current selection:

```bash
specgate user current
specgate workspace current
```

`workspace current` shows the selected workspace and whether it came from a
command override, environment override, current Git project binding, or global
config.

Select a different workspace:

```bash
specgate workspace list
specgate workspace select
specgate workspace select <slug>
```

Bind the current Git checkout to the currently selected workspace:

```bash
specgate workspace bind
```

Or bind a named workspace directly:

```bash
specgate workspace bind <slug>
```

User/workspace selection is attribution and filtering, not authentication. New
quick work items receive the selected `created_by` and `workspace_id`.

When you run `specgate workspace select` from inside a Git checkout, the
interactive CLI asks where to save the selection:

- **This project** — use the workspace automatically whenever `specgate` runs
  from that checkout or any subdirectory.
- **Global default** — use the workspace when no project-specific workspace is
  bound, and clear this checkout's existing project binding if one exists.

Project bindings live in the user CLI config keyed by the Git root path; the
command does not write files into the repository. Non-interactive modes
(`--plain`, `--json`, or `--no-input`) skip the save-scope prompt, save the
global workspace, and clear this checkout's project binding when one exists.
Use `workspace bind` for scripts, IDE agents, and any case where the intent is
definitely "this project."

Use a one-command override when you need to stay in the current project but
inspect another workspace:

```bash
specgate --workspace platform status
SPECGATE_WORKSPACE=platform specgate work list
```

Remove only the current Git checkout's binding:

```bash
specgate workspace unbind
```

## Find work that needs attention

```bash
specgate status
specgate work list
```

Both commands use the project-bound workspace when one exists for the current
checkout, otherwise the global selected workspace. `--workspace` and
`SPECGATE_WORKSPACE` override both for one command. If no workspace is selected,
select or bind one first. Use the global view only when you mean it:

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

## Complete an existing change

For the normal post-handoff path, follow this sequence. The Change facade reads
the existing work item and delivery history; it does not create a new durable
Change entity.

1. Check the current state and follow the shown next action:

```bash
specgate change status <work-ref>
```

The status keeps **Evidence**, **Assurance**, **Decision**, and **Receipt**
separate, alongside freshness, missing requirements, the next actor, and an
exact next command. A passing check is not human acceptance.

2. If implementation or rework is required, scaffold the default completion
report and fill in the coding agent name, affected files, checks, criterion
claims, and evidence:

```bash
specgate delivery report <work-ref> --init
```

For a file-safe ref (letters, digits, `-`, and `_`), this creates
`.specgate/completion-<ref>.json`. For an unsafe ref or a different location,
use the explicit `--file` form when submitting.

3. Submit using the default completion path:

```bash
specgate change submit <work-ref>
```

Use `specgate change submit <work-ref> --file <completion.json>` only when the
default path cannot be derived or is not the file you filled.

4. Check status again after the delivery tail finishes:

```bash
specgate change status <work-ref>
```

5. A human reads the separate trust signals. Run exactly one of the following human decisions. If status reports `State: awaiting_acceptance`, the
evidence is ready for an acceptance decision:

```bash
# Local
specgate --yes change accept <work-ref> --note "Approved after review"

# Full
specgate change accept <work-ref> --note "Approved after review"
```

If status reports `State: awaiting_review`, run its detailed, read-only
`specgate delivery status <work-ref> --detail` command first. Inspect the
unclear criteria, then either accept after independent confirmation or request
rework.

If status reports `State: review_pending`, a newer completion exists but its
delivery review has not finished. Run the exact `specgate delivery review
<work-ref>` command returned by status, then check status again. Never accept
the older completion's review.

If the status is not ready for acceptance, or the reviewer finds a gap, request
rework instead:

```bash
# Local
specgate --yes change request-changes <work-ref> --note "Please address the failing check"

# Full
specgate change request-changes <work-ref> --note "Please address the failing check"
```

Local mode always requires `--yes`; Full mode confirms interactively when
possible. Do not run both decisions for one review. A rejection remains
authoritative for that exact completion; submitting corrected evidence starts a
new review and human-decision cycle.

Use the expert families when you need their detailed or troubleshooting views:
`delivery report` to scaffold or inspect the delivery tail, `delivery status
--detail` for the raw review readback, `work context` for the implementation
contract, `gates` and `artifact` for readiness and approval details, and
`audit` or `verify` for history or closeout. `change prepare` is not available;
continue to use the artifact and gate commands for those detailed preparation
steps.

## Create a quick work item

In either mode, use the quick route for small, understood work. Local mode
requires at least one explicit `--ac`; Full mode can draft missing criteria
when its platform model is configured. An IDE agent following the SpecGate
preparation skill instead shows you explicit criteria for confirmation before
creating the work item in either mode.

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

The quick route skips an artifact package. It still creates governed context
and delivery review. It is immediately pickup-ready; discover it later with
`specgate work list --phase ready`.

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

Preview an explicitly mapped local package first:

```bash
specgate artifact publish --file artifact.json --preview --json
```

The preview expands local `source_file` entries and reports preserved paths,
explicit roles, optional provenance, target, and update base version. It never
uploads or calls the server. After human confirmation, run the same command
without `--preview`. Framework names and directory layouts have no effect on
storage behavior.

For an update, compare with one explicitly selected artifact:

```bash
specgate artifact publish --file artifact.json --preview --compare <base-artifact-id> --json
```

This opt-in preview makes read-only calls for artifact metadata and stored file
hashes. It never downloads old content or publishes. Results classify paths as
added, removed, changed, or unchanged. `base_version` in `artifact.json` must
match the selected artifact.

The preview also lists `impact_declaration` under `omitted` when a noninteractive
publish file has no impact answers. Missing or `unknown` impact can select
stricter governance. Add honest `yes`, `no`, or `unknown` answers as documented
in [Customize governance](customize-governance.md), then preview again; do not
default uncertain fields to `no`.

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
does not dereference local file URLs itself. Each document must set exactly one
of `content`, `source_file`, or `file_url`; `path` alone never implies a local
source file.

Run readiness checks:

```bash
specgate gates check <artifact-id> --json --summary
```

The summary is optimized for IDE-agent context. Read stored detailed evidence
without rerunning checks only when needed:

```bash
specgate gates results <artifact-id> --json
```

Readiness is not approval. Approval is a separate governed action.

## Review and approve from the CLI

Approve an artifact version and make it canonical, or send it back with a note.
The normal approval path is:

```bash
specgate --yes change approve <artifact-id> --note "LGTM" \
  --title "<work title>" --ac "<confirmed criterion>"
specgate artifact request-changes <artifact-id> --note "Tighten the error copy"
```

Both record the selected user as the deciding actor. `artifact
request-changes` is Full-only. In Full mode, interactive terminals ask for
confirmation first while plain, JSON, and non-interactive runs proceed
directly. Local human approval always requires explicit `--yes`.

The normal decision creates the feature-backed handoff and returns its work
reference. Read it before implementation:

```bash
specgate work context <work-ref> --json
```

The expert `artifact approve` and `artifact promote` commands keep both
transitions separately available for diagnosis. The created work item's
`lead_artifact_id` must equal the promoted artifact, and its Context Pack must
contain the governed spec/design/plan/verification material or exact artifact
references. Comparison, publication, approval, promotion, work linkage, and
Context Pack assembly remain different durable states even though the normal
approval command resumably coordinates the last four.

`artifact show` also accepts a unique id prefix from the `artifact list` table:

```bash
specgate artifact list
specgate artifact show <id-prefix>
```

`artifact list` shows current artifacts by default: superseded versions are
hidden until you ask for them explicitly.

```bash
specgate artifact list --status all         # every status, including superseded
specgate artifact list --status superseded  # only superseded versions
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
specgate delivery report <work-ref> --init --json
```

Copy the response's exact `data.path` into `COMPLETION_PATH`; do not derive it
from an arbitrary work reference.

Bare `--init` writes `.specgate/completion-<ref>.json` — a transient scaffold
under the repo-local `.specgate/` working directory, which is gitignored so it
is never committed. (A committed `.specgate/config` for shared team defaults is
a separate, optional file; see
[Configuration → Repo-level `.specgate/` directory](../reference/configuration.md#repo-level-specgate-directory).)
Fill the scaffold with:

- the coding agent's stable name in `agent.name`;
- summary;
- affected files;
- checks;
- per-criterion claims and evidence.

The scaffold is deliberately incomplete: `agent.name` starts blank,
`affected_files` starts empty, checks start `skipped`, and criterion claims start
`not_done`. A completion needs a named coding agent, a `satisfied` claim needs
non-empty criterion evidence, and every reported passing check needs a runnable
`command`; invalid reports stop before any network call.

Submit the whole delivery tail (`<ref>` is your work reference):

```bash
specgate change submit <work-ref> --file "$COMPLETION_PATH"
specgate delivery status <work-ref> --detail
```

For quick-route work, include the persisted verdict from `delivery status` in the completion receipt.

For one machine-readable closeout check, use:

```bash
specgate verify <work-ref> --json
```

It reports the exact governing artifact version, every acceptance-criterion
verdict, automated checks, delivery verdict, cleanup eligibility, and the next
command. It is read-only and exits `1` while the work is not closeable.

To find governed specifications that have no corresponding delivered work, or
were delivered only against an older version, run:

```bash
specgate coverage --json
```

To move completed Local governance history into an empty Full workspace, export
a checksummed bundle, switch to Full mode, select the exact destination
workspace, and always inspect the dry-run before confirming:

```bash
specgate portable export --file ~/specgate-local.json
specgate portable import --file ~/specgate-local.json --dry-run
specgate portable import --file ~/specgate-local.json --yes
```

Portable import reads at most 64 MiB and preserves each feature's exact Local
canonical artifact plus every work item's exact approved artifact binding. It
does not synchronize, transfer credentials, merge workspaces, or resolve
conflicts. Source gate verdicts are historical only; Full mode dispatches fresh
destination gate tasks for every imported artifact instead of accepting Local
verdicts. Unpromoted artifacts remain drafts.
It is not transactional, but exact partial imports are resumable by recorded
source workspace, record IDs, and digests. If an import fails after writes
begin, repeat the dry-run; the retry also finds auto-archived imported work,
does not duplicate an identical human decision, and reports changed or
ambiguous records as conflicts.

### Resume safely on another checkout

A fresh IDE agent should load the persisted receipt before resuming:

```bash
specgate delivery status <work-ref> --detail --json
```

Compare `git_receipt.repository`, `branch`, `head_revision`, and `diff_digest`
with the checkout it is about to use. Stop and ask for human direction on any
mismatch; do not treat a receipt as permission to continue or as source code.
The receipt stores metadata and a digest only. It is self-attested
(`agent_attested`) evidence that detects stale/drift between report and submit
and gives a resume comparison target—not cryptographic proof. A marked, merged
PR/MR corroborates repository activity only when its head SHA matches the
latest submitted `git_receipt.head_revision`.

`delivery submit` reports completion, runs gates, triggers delivery review, and
prints the resulting status. Human-readable status keeps evidence, assurance,
human decision, and the recorded Git receipt separate. The output also reminds
you that the reported checks are self-selected by the coding agent — delivery
review judges only the checks you report, so run a full-module check for
cross-cutting changes rather than a narrow subset. If review fails, fix the
named gap, update evidence, and submit again.

Before anything is sent, the CLI verifies that every `criteria[].evidence.path`
exists in the working tree (run it from the repo root) — the delivery review
judge cannot read your checkout, so citations are checked on your machine
instead. For each cited local file it also stores a short excerpt plus a
SHA-256 digest under `evidence.grounding`, which lets the delivery review show a
`grounded` per-criterion trust tier. Missing `affected_files` entries only warn,
since deletions are legitimate; `--skip-evidence-check` bypasses verification
and grounding.

Add `--run-checks` to re-execute every non-skipped `checks[].command` locally
and submit the observed pass/fail results instead of claimed statuses. Keep
`name` as a readable label such as `tests`, `lint`, or
`build`; put the shell command, such as `go test ./...`, in `command`. Executed
checks are stronger evidence than narrated ones. Skipped checks are left
untouched. The CLI displays the commands before an interactive confirmation;
automation must review the completion file and pass `--yes`.

When the server-side model is disabled or unavailable, the verdict is derived
from the coding agent's own claims: it is labeled `agent_attested`. A would-be
pass pauses for a different agent's receipt-bound `delivery peer-review` or
human review; grounded local citations do not waive that stop. A configured
platform model adds another review of the submitted evidence, but it does not
inspect the code, replace CI, or make the human acceptance decision. Artifact
readiness gates can instead be dispatched to your IDE agent with
`specgate gates tasks dispatch <artifact-id>`.

## Measure governance value

```bash
specgate stats
specgate stats --days 90 --all-workspaces
```

`stats` reports reviewed items, first-pass yield, pre/post-build governance
signals, rework, blocked-ambiguity reports, cycle time, and a recent signal
ledger. These counts are projections from recorded gate, review, and
blocked-ambiguity events; they are signals for human interpretation, not
adjudicated proof that SpecGate prevented a defect or saved ambiguity. It is most
useful after several work items have completed delivery review.

## Use the CLI in automation

Rules for scripts:

- use `--json --no-input`;
- pass all required values explicitly;
- set `--server` or `SPECGATE_SERVER`;
- read command results from stdout; progress and warnings use stderr;
- store command output as evidence when useful;
- do not parse human output.

Example:

```bash
specgate --json --no-input --server "$SPECGATE_SERVER" \
  delivery submit "$WORK_REF" --file "$COMPLETION_PATH"
```

Set `COMPLETION_PATH` from the preceding `delivery report --init --json`
response's `data.path`; do not derive it from an arbitrary work reference.

## Update or diagnose the CLI

```bash
specgate version
specgate doctor
specgate model test
specgate update
```

In Local mode, `doctor` verifies the embedded store, user, and workspace without
a server or network connection. In Full mode, it reports server compatibility
plus setup state for identity, workspace, the selected workspace's board,
model, and IDE plugin follow-up. It can still report a missing workspace before
a selection exists. `model test` is a settings-only check: it verifies that
model provider, model, and API-key settings are present and does not contact the
model provider. `update` refreshes the CLI, already-installed global IDE plugin
files, and a CLI-managed Full appliance when one exists. Project-local IDE
files are scoped to each repository and are not changed implicitly; refresh
them from that repository with `specgate plugins install --project-local`.

## Related

- [Quickstart](../quickstart.md)
- [CLI reference](../reference/cli.md)
- [Use SpecGate with a coding agent](coding-agent-workflow.md)
