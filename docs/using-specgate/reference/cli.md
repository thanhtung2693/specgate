# CLI reference

The `specgate` CLI is the stable command surface for users, automation, and
coding IDE agents. It governs the handoff from source specs to reviewed,
verifiable delivery.

For exact flags in your installed version:

```bash
specgate <command> --help
```

## Full-mode server selection

Local CLI mode does not create an HTTP client or use a server. The precedence
below applies to Full appliance and remote-server workflows.

Highest precedence first:

1. `--server <url>`
2. `SPECGATE_SERVER`
3. the repo's committed `.specgate/config` server
4. saved user configuration
5. `http://localhost:3000/api/doc-registry`

`model set` ignores a repo-configured server because it may transmit a provider
API key. Use `--server`, `SPECGATE_SERVER`, or saved user configuration to choose
that command's trusted destination. Its optional OpenRouter model search times
out after 30 seconds and rejects catalog responses larger than 4 MiB; if search
is unavailable, interactive setup falls back to manual model-ID entry.

Set the saved URL:

```bash
specgate config server http://localhost:3000/api/doc-registry
```

## Output modes

| Mode | Selection | Use |
|---|---|---|
| Human | default | interactive terminal work |
| Plain | `--plain` | stable text without color or prompts |
| JSON | `--json` | automation and IDE tools |

Human output uses a compact semantic color hierarchy, Unicode status symbols,
section headers, and progress summaries when stdout is an interactive terminal.
It automatically falls back to the same readable text without ANSI escapes when
output is redirected or piped, in CI, with `NO_COLOR` set, or with `TERM=dumb`.
Use `--plain` to request portable text explicitly. `--plain` and `--json` never
include color escapes or Unicode dashboard glyphs; `--json` also implies
`--no-input` and emits only its final JSON envelope on stdout. `--plain` also
disables interactive prompts, so commands that need user input require explicit
flags or a file body in plain mode.
Invalid flags and positional arguments return a `usage` error with exit code
`2`; in `--json` mode both use the same JSON error envelope as command
validation failures and include the relevant `--help` command.

Other global flags:

- `--version` — print the installed CLI version;
- `--workspace <slug-or-id>` — use a workspace for this command only;
- `--yes` — accept confirmations;
- `--no-input` — fail instead of prompting;
- `--timeout <duration>` — request timeout;
- `--json-progress` — with `--json`, emit progress events before final JSON
  output.
- Commands reject unexpected positional arguments instead of silently ignoring
  them.
- JSON errors set `error.transient=true` for service-unavailable and network
  failures so automation can retry; validation, conflict, and governance
  failures remain non-transient.

`user login --workspace <name>` is the exception: on that command the flag names
the workspace to create or reuse during local identity setup.

Product work and artifact commands require a resolved workspace. If no
workspace is selected, the CLI stops before sending the product request instead
of returning or requesting unscoped WorkBoard data.

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

The root help groups commands by audience. Start with `change` for a normal
post-handoff delivery path, then use the core workflow lane when you need the
underlying detail.

### Start here

| Family | Purpose |
|---|---|
| `change` | The normal approval and post-handoff facade in Local and Full mode. Use the dedicated [Change facade](#change-facade) reference for exact syntax, output fields, and confirmation behavior. |

### Core workflow

| Family | Purpose |
|---|---|
| `status` | Show selected-workspace board counts, attention count, and next action; `--all-workspaces` makes a cross-workspace read explicit |
| `stats` | Show a governance-value readout — reviewed items, first-pass yield, pre/post-build governance signals, rework, blocked-ambiguity reports, cycle time, and a recent signal ledger. These counts are projections from recorded gate, review, and blocked-ambiguity events; they are signals for human interpretation, not adjudicated proof that SpecGate prevented a defect or saved ambiguity. `--days N` sets the window; `--all-workspaces` makes a cross-workspace read explicit |
| `capabilities` | Print the stable Local/Full capability manifest. Each capability is `available`, `unavailable`, or `configuration_required`; Full-mode governance chat reflects its own live health/configuration rather than merely the presence of the Agents service. `--json` is suitable for IDE agents and automation |
| `coverage` | Classify every canonical specification in the selected workspace as `uncovered`, `unfinished`, `stale`, or `delivered`, including the governing artifact, related work refs, and a next command for every non-delivered state |
| `portable` | `portable export --file <path>` writes the selected Local workspace to a private, checksummed `specgate.portable/v1` bundle. Export and import both enforce a 64 MiB bundle limit; an oversized export does not replace an existing destination, and export refuses the active Local SQLite database or journal paths. In Full mode, `portable import --file <path> --dry-run` reports every destination conflict without writing; re-run with `--yes` only after reviewing an empty conflict list. A retry reuses only exact prior imports proven by source workspace, record IDs, and digests |
| `verify` | Produce one read-only closeout verdict for a work ref: artifact-backed work includes the exact artifact version and digest, while artifact-free work sets `quick_route=true`; both include criterion evidence, checks, authoritative delivery verdict, cleanup eligibility, and the next command. A non-closeable result exits `1` |
| `work` | List the attention queue (`work list --all-workspaces` is an explicit aggregate status read), or enumerate ready work in the selected workspace with `work list --phase ready` to discover pickup-ready items and their refs. Ready includes approved artifact-backed work and quick-route bug fixes with no lead artifact. The aggregate and phase flags cannot be combined. Both modes can create feature-backed or quick work and read its immutable Context Pack; Local quick work requires at least one explicit `--ac`. `work policy`, archive, and server-backed resolution are Full-only |
| `audit` | `audit <ref>` prints the full governance trail for a work item — the "git log for governance". It resolves a change-request id or key (e.g. `CR-1234`) and renders one line per event (date, actor+kind, action, verdict, trust tier, detail) sorted oldest-first, merging artifact status events, readiness/quality gate runs, delivery reviews, and workboard lifecycle events. Read-only; `--json` prints the raw `AuditTrail`; `--verify` recomputes the tamper-evidence event chain and reports `intact` or `tampered` (naming the first bad event). Unresolvable ref → clean error |
| `feature` | List and inspect governed features in either mode so a new artifact can reuse the exact key instead of creating a duplicate. In Full mode, `feature list` hides archived features unless `--all` is set, and `feature archive <key-or-id>` retires a feature with confirmation while keeping its record and history |
| `knowledge` | Full mode only: list, inspect, add, and search workspace-scoped Governance Knowledge documents. `knowledge add-text --title <title> --file <path>` uploads a text/Markdown source into the selected workspace; `knowledge search <query>` retrieves cited chunks using the configured embedding model |
| `artifact` | Preview (`artifact publish --preview`), optionally compare that preview with one explicit base artifact (`--compare <artifact-id>`), publish complete immutable versions, inspect, and read artifacts; `artifact coverage <artifact-id>` is a read-only exact-version delivery view; decide as a human reviewer with `artifact approve` in either mode or `artifact request-changes` in Full mode (optional `--note`); `artifact show` accepts a unique id prefix from `artifact list` |
| `gates` | Artifact readiness checks (`check`), stored artifact results (`results`), and IDE-agent artifact gate tasks (`tasks`) work in both modes. Local IDE gate tasks and Full model-less IDE gate tasks use the same `check` → `tasks list/show/submit-result` → `results` loop. Compact `gates check --summary` output preserves each result's executor origin so agent-attested evidence is not presented as platform evaluation. Work-item model gates (`run`, `status`, `history`) are Full-only |
| `delivery` | Report implementation, run delivery review, and decide delivery as a human reviewer; `delivery report --init` scaffolds a conservative completion report (`.specgate/completion-<ref>.json`) with a required blank `agent.name`, empty affected files, one `pending` check per unique declared verification binding (or a `tests` fallback), and `not_done` criterion claims. `pending` is a scaffold-only placeholder: submission rejects it until the agent records `pass`, `fail`, or an explained `skipped`. The scaffold captures Git identity but defers delivery-scope comparison until `affected_files` is supplied at submission. Fill `agent.name` with the coding agent's stable name before submission. `delivery peer-review --init` scaffolds a review bound to the latest completion event and its exact Git receipt; its reviewer must cover every canonical criterion exactly once, then `--file` records it and reruns review. This works in both Local and Full mode: the peer must differ from the completion agent, and the CLI preserves the receipt in the bound scaffold so the server can verify the same completion. Direct `delivery report --file` is Full-mode feedback ingestion; Local completion uses `change submit`. `delivery submit` accepts only `coding_agent.completed`; both paths reject missing completion-agent identity, untouched pending checks, `satisfied` claims without evidence, and passing checks without runnable commands before any network call. `delivery submit` runs the whole tail (report → gates → review → status), while `delivery approve` / `reject` record human decisions. A human may accept an advisory or false-negative review without hiding its evidence verdict; the completion reporter cannot approve its own delivery, and a new completion needs its own review and human decision. These rules are the same in Local and Full mode. Human `delivery status --detail` output separates evidence assessment, assurance source, human decision, and recorded Git receipt; JSON exposes the authoritative `verdict` and optional `evidence_verdict`. Cited local evidence paths are existence-checked and grounded with an excerpt plus SHA-256 digest; `submit --run-checks` previews and confirms the completion file's commands before re-executing non-skipped checks through `sh -c`, marks those rows as observed by the SpecGate CLI, and submits their observed statuses. Local status then labels that assurance as `locally reproduced`. Noninteractive callers must add `--yes`. Git receipts retain full checkout provenance while labeling changes outside `affected_files` as unrelated checkout state. `change status` compares the stored receipt with the current repository, branch, HEAD, and diff digest; mismatches are explicit stale warnings and unavailable Git metadata never claims freshness. `agent_attested` passes require a bound peer review or human review. |

## Change facade

`change` is the normal view over existing artifact, work, and delivery records;
it does not create a new durable Change entity. Its exact commands are:

```bash
specgate change status <work-ref>
specgate --yes change approve <artifact-id> --title <title> --ac <criterion> [--ac <criterion>...] [--description <text>] [--note <note>]
specgate change submit <ref> [--file <completion.json>] [--run-checks] [--skip-evidence-check]

# Local: explicit human assertion is required.
specgate --yes change accept <ref> [--note <note>]
specgate --yes change request-changes <ref> [--note <note>]

# Full: interactive terminals confirm; non-interactive callers may run these directly.
specgate change accept <ref> [--note <note>]
specgate change request-changes <ref> [--note <note>]
```

For `change submit`, a file-safe `<ref>` defaults to
`.specgate/completion-<safe-ref>.json`. A file-safe ref contains only letters, digits, `-`, and `_`. An unsafe ref must pass `--file` with a completion file path.
Use the default submission form for normal work and the explicit file only when
the ref is unsafe or the completion file is intentionally elsewhere:

```bash
specgate change submit CR-123
# Full mode, when this stored issue URL resolves to the work item:
specgate delivery report https://github.com/acme/shop/issues/42 --init --json
COMPLETION_PATH="<exact data.path from the preceding response>"
specgate change submit https://github.com/acme/shop/issues/42 --file "$COMPLETION_PATH"
```

`--run-checks` re-executes non-skipped checks through `sh -c` after previewing
their commands; noninteractive callers must also pass `--yes`. Use
non-interactive POSIX shell commands. `--skip-evidence-check` bypasses
local citation validation and should be reserved for recovery when the cited
paths cannot exist in the current checkout.

`change approve` is the normal feature-backed preparation decision in both
modes. It records approval of the exact artifact snapshot, promotes that
version to the feature canonical, creates or reuses its artifact-bound work
item, and verifies the derived Context Pack in one resumable transition. The
title and every repeated `--ac` must come from the human-reviewed preparation;
SpecGate does not extract or invent criteria from document prose. Its receipt
includes the exact `artifact_version`, `snapshot_digest`, `work_ref`, and
Context Pack state. An exact retry reuses the existing work; if that work is
already delivered, the receipt reports `already_delivered` and routes to
`change status` instead of presenting it as new implementation. The expert
`artifact approve`, `artifact promote`, and `work create` commands remain
available separately.
`change submit --json` returns the compact actionable Change status; use
`delivery submit --json` when troubleshooting requires the full report, gate,
review, and delivery-status payloads.

`change status` gives the next safe action. Human output names the same concepts
as the JSON fields below; JSON emits the standard success envelope with this
object in `data`.

| JSON field | Meaning |
|---|---|
| `mode` | `local` or `full`, the mode used to derive the status and command. |
| `ref` | Resolved work reference used by follow-up commands. |
| `title` | Existing work item's title. |
| `state` | One of the action states described below. |
| `evidence` | Summary of delivery evidence; it is not a human decision. |
| `assurance` | Summary of the review/assurance source; it is not proof that a human accepted delivery. |
| `decision` | Recorded human delivery decision, if any. |
| `receipt` | Recorded Git receipt summary, if any. |
| `freshness` | Whether the stored evidence was checked against the current checkout. |
| `next_actor` | Actor expected to take the next action: `implementing_agent`, `human_reviewer`, `maintainer`, or `none`. |
| `missing` | Always an array of named requirements still missing; empty after acceptance. |
| `guidance` | Optional human rework note, preserved for the implementing agent after `request-changes`. |
| `stale` | Whether peer-review evidence is stale or the current checkout differs from the stored Git receipt. |
| `stale_reason` | Optional; omitted unless `stale` is true. |
| `next_command` | Exact next CLI command for the returned state and mode. |

`evidence`, `assurance`, `decision`, and `receipt` are separate summary labels:
evidence says what was reported or reviewed, assurance says how it was assessed,
decision says what the human recorded, and receipt identifies the stored Git
provenance. Do not treat one as authority for another.
The expert `delivery status --json` readback likewise keeps authoritative
`verdict` separate from optional `evidence_verdict` on a human decision.
Optional `assurance_sources` reports `repository_observed` only when a merged
PR/MR event's normalized `head_sha` matches the latest completion receipt's
normalized `git_receipt.head_revision`; that signal does not mean a human
accepted the delivery. CI is not a first-release assurance source.

| `state` | Action semantics |
|---|---|
| `implementation` | An implementing agent must add or repair delivery evidence; `next_command` scaffolds the delivery report. |
| `awaiting_review` | Evidence needs human interpretation; `next_actor` is the human reviewer and `next_command` opens the detailed criterion review, which ends with the exact accept and request-changes commands. |
| `review_pending` | A newer completion has not received its own delivery review; the implementing agent runs the returned review command before any human decision. |
| `awaiting_acceptance` | Evidence passed but a human must decide. Local emits `specgate --yes change accept <ref>`; Full emits `specgate change accept <ref>`. |
| `accepted` | A human accepted the current reviewed completion. The evidence label still discloses an advisory or false-negative assessment; no actor is pending and the next command is the audit trail. |
| `rework_requested` | A human requested rework. Follow `guidance`, revise the result, then submit a new completion for its own review cycle. |
| `blocked` | A maintainer must restore delivery policy; the next command opens detailed delivery status. |

`stale` does not replace the delivery decision: it flags stale peer-review
evidence and explains why in optional `stale_reason`. Follow `next_command`,
and use `missing` and `next_actor` to decide whether to implement, review, or
escalate. Local acceptance or rework always requires `--yes`; Full confirms in
an interactive terminal and otherwise accepts the explicit command.

For detailed preparation or troubleshooting, keep using the expert `delivery`,
`work`, `gates`, `artifact`, `audit`, and `verify` families. `change prepare` is
not available: agents still prepare and preview the explicit artifact and work
contract before the human runs `change approve`.

`specgate model off` deliberately enables model-less operation while preserving
the selected provider/model/key. `specgate model on` enables it again.

`work create --feature <key-or-id> --title <t>` creates a feature-backed work
item bound to the feature's approved canonical artifact (the lead artifact is
set automatically; the feature must have a promoted canonical). The IDE agent
reads every explicitly mapped source spec document and submits each confirmed
criterion with a repeated `--ac`. SpecGate requires a non-empty explicit list
and does not infer criteria from headings, numbering, filenames, or keywords.
This works in both Local and Full mode.

`artifact coverage <artifact-id>` reports work linked to that exact immutable
artifact version—not merely the same feature. Its state is `uncovered`,
`in_progress`, `delivered`, or `superseded`. `delivered` requires every linked
work item to be delivered, including work auto-archived after approval; it is
informational and never changes work or artifact state.

Use workspace coverage and closeout verification before removing transient
spec-work files. Workspace coverage marks a canonical specification `delivered`
only when every work item bound to that exact canonical artifact is delivered:

```bash
specgate coverage --json
specgate verify <work-ref> --json
specgate cleanup --work --dry-run
```

`coverage` never infers links or creates work. `verify` resolves the work only
inside the active workspace (or the exact `--workspace` override), never
approves delivery, and never removes files. Cleanup eligibility is true only
after the work has an explicit human delivery approval in either mode. An
advisory or false-negative evidence verdict remains visible but does not erase
that human authority.

### Move Local data into Full mode

Portable bundles are an explicit one-way move, not synchronization:

```bash
# While Local mode is selected:
specgate portable export --file ~/specgate-local.json

# Initialize Full mode, then select the exact destination workspace.
# This changes the active CLI topology; it does not delete Local SQLite state.
specgate init --mode full
specgate workspace select <destination-workspace-slug>

# Preview first. This writes nothing:
specgate portable import --file ~/specgate-local.json --dry-run
specgate portable import --file ~/specgate-local.json --yes
```

The versioned bundle contains governed artifact documents, their content
digests, feature/work relationships including each feature's exact canonical
artifact, criteria, IDE gate evidence, delivery evidence, and source-workspace
metadata. Request types use the canonical API enum; work, gate, and delivery
records retain exact persisted relationships. It contains no settings, provider
keys, credentials, tokens, or integration secrets and is created with
user-only file permissions. Import validates the bundle checksum and
relationships before contacting the server. It refuses ambiguous or absent
workspace/user mapping,
reports all existing feature and prior-import conflicts before mutation, and
never matches by keyword or similarity and never merges or resolves conflicts
automatically. Unpromoted Local artifacts remain drafts and do not become
canonical during import. If a destination canonical changed after a partial
import, or the resumed feature gained a destination-only artifact version, the
next dry-run reports a conflict instead of overwriting or publishing past that
destination change.
Exact partial imports are resumable: a retry reuses artifacts only when their
recorded source workspace, artifact ID, and digest match, and work only when its
source-workspace-qualified structured reference, lead artifact, title, and
ordered criteria match. Archived imported work remains eligible for exact retry
matching, and an identical persisted human decision is not recorded twice.
Source gate evidence remains in the export as history, but import never submits
those verdicts against destination policy. Every imported artifact dispatches
fresh destination gate tasks for the IDE agent to evaluate after restoring its
approved/draft state, so approval-only gates are not skipped.
Acceptance-criterion IDs map through the exact ordered contract
that the destination returns after creating the same criteria. The success
receipt maps Local artifact/work IDs to their created or exactly reused Full IDs.
Imported completion and peer-review evidence is translated to those destination
IDs, then reviewed against the destination policy before an imported human
decision is replayed.

Import performs a sequence of immutable API writes, not one database
transaction. If it fails after writes begin, inspect the destination before
retrying; the next dry-run reuses exact prior writes and reports changed or
ambiguous records as conflicts.

For Local IDE gate tasks and Full model-less IDE gate tasks, run:

```bash
specgate gates check <artifact-id> --json --summary
specgate gates tasks list <artifact-id> --json
specgate gates tasks show <task-id> --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
specgate gates results <artifact-id> --json
```

A successful invocation may return `aggregate=not_run` until every required
semantic result is submitted. That is a pending readiness state, never a pass.
`dispatched_to_ide_agent.pending_task_ids` is the authoritative unfinished-task
receipt in both modes; `created_task_ids` only records what that invocation
created and may be empty on an idempotent repeat.

`work create-quick <title>` uses the title as the description when
`--description` is omitted, so the shortest quickstart form still satisfies the
server contract. Repeat `--ac` on `work create-quick` or feature-backed
`work create` to supply human-authored acceptance criteria.
Append a trailing `@check:<name>` token to bind that criterion to a delivery
report check, for example
`--ac "POST /imports rejects bad CSV rows @check:integration"`. The check name
must match a later `delivery report` / `delivery submit` `checks[].name`; the
delivery reviewer treats that criterion deterministically from the submitted
check result. Do not invent bindings on behalf of the human.

`specgate artifact approve` reports governance-policy refusals with exit code
`1`. Artifact approval always requires an explicitly asserted human actor.
This is audit attribution inside the trusted deployment, not identity-secure
authorization.

Full-mode Knowledge commands require an active workspace from `--workspace`,
`SPECGATE_WORKSPACE`, a project binding, or the global workspace. Typical use:

```bash
specgate knowledge add-text --title "Checkout policy" --file docs/checkout.md --type policy_doc --authority high
specgate knowledge list
specgate knowledge search "checkout refund rules" --context section --limit 5
specgate knowledge link doc_123 --request CR-456
specgate knowledge unlink doc_123 --request
specgate knowledge show doc_123
```

Use `--json` for full document/search DTOs. Search results include
`specgate://knowledge/...` citation URLs so agents and reviewers can trace the
retrieved context back to the indexed document version. Link/unlink commands
create a new Knowledge version with updated feature/work-item links; they do not
mutate the previous version's curation metadata. `knowledge list` returns a
page; when more documents match than the page contains, the human output tells
you the next `--offset` to use. JSON output keeps the full `total` match count
alongside the page items.

### Setup and identity

| Family | Purpose |
|---|---|
| `doctor` | Check the selected Local store or Full server setup and print the exact recovery command when setup is incomplete |
| `config` | Save CLI configuration |
| `version` | Print the installed CLI version; equivalent to `specgate --version` |
| `model` | Configure Full-mode governance and embedding models plus provider keys |
| `user` | Create/select the local user with `user login`, clear it with `user logout`, list users, and show the selected user |
| `workspace` | List workspaces, list members, show/select the active workspace, bind the current Git project, and unbind the current project |
| `plugins` | Install and verify Codex, Claude Code, and Cursor IDE plugin files. Local mode reads the complete matching package embedded in the CLI; Full mode reads it from the configured appliance |
| `skill` | Inspect user-defined Skills |
| `open`, `update`, `uninstall` | Open the web UI — `open` alone opens the base URL; `open <work-ref>`, `open reviews\|artifacts\|work`, and `open --artifact <id>` deep-link to the matching page; add `--print` to return the server-advertised canonical URL without launching a browser — refresh setup, or remove user-local setup |

The selected local user and workspace are stored in CLI config. Use
`specgate user login --workspace <name> --display-name <name> --username <name>`
to create or reuse the local user/workspace pair when connecting to an existing
server.
`specgate user logout` clears the local user, global workspace, and
project-bound workspace selections without deleting server data. They are used
for attribution and default workspace filtering, not authentication: Full-mode quick work
items receive `created_by` and `workspace_id`, artifact publish sends the
selected username as `created_by` when the body does not already include it,
archive actions use the selected username as the actor. `status`, `stats`, and
the default `work list` attention view may make an explicit aggregate read with
`--all-workspaces`; `work list --phase` always requires the selected workspace.
Product commands, including gate-task operations, require a selected
workspace; no unscoped compatibility path exists in development.

For multi-project use, run `specgate workspace bind` inside a Git checkout to
bind that checkout to the currently selected workspace. Use
`specgate workspace bind <slug>` to bind a named workspace directly. The CLI
records that workspace in the user config under the checkout's Git root path.
Future commands run from that checkout or its subdirectories use the project
workspace instead of the global workspace. Interactive `specgate workspace
select` can do the same thing by choosing **This project** at the save-scope
prompt, or it can choose **Global default** to keep the previous
single-workspace behavior and remove the current checkout's project binding
when one exists. Plain, JSON, and no-input `workspace select <slug>` commands
do not prompt; from inside a Git checkout, they save the global workspace and
clear that checkout's binding. Use `workspace bind` when automation or an IDE
agent should bind the current project.

Use `specgate workspace current` to see the active workspace and whether it
came from an override, project binding, repo default, or global selection. When
the current checkout is using the global workspace, `workspace current` shows
the Git root as unbound and prints the matching `workspace bind` command. Use
`specgate workspace members` to list the effective workspace's member rows,
including a `(you)` marker when the selected local user matches
`workspace_members`. The command is read-only: it does not create users,
workspaces, or memberships, and it is audit/team visibility, not authorization.
Use `specgate workspace unbind` inside a Git checkout to remove only that
project's workspace binding. Effective workspace precedence is:

1. `--workspace <slug-or-id>`;
2. `SPECGATE_WORKSPACE`;
3. current Git project binding;
4. repo `.specgate/config` workspace default;
5. global workspace;
6. no workspace.

### Full appliance

| Family | Purpose |
|---|---|
| `init --mode full`, `up`, `down`, `local-status` | Manage a Full appliance deployment |
| `demo remove` | Delete the demo data created by `specgate init` seeding in the selected workspace (idempotent) |
| `cleanup` | Without flags, Full-mode housekeeping cleanup with confirmation: immediate retention sweep, demo seed removal, and archived work-item purge. `cleanup --work` is local and mode-independent: it selects transient entries under the current project's `.specgate/work` plus generated `.specgate/completion-*.json` and `.specgate/peer-review-*.json` scaffolds. `cleanup --backups` selects only `specgate-before-*.tar.gz` recovery archives under the saved deployment directory; use `--dir` to choose another managed deployment. Both file modes support `--dry-run`, repeatable `--item`, interactive selection, and noninteractive `--yes`. Noninteractive work cleanup requires explicit `--item` values; bare `--yes` never removes every work entry. They never remove `.specgate/local-stack`, project configuration, active appliance data, or unrelated files. Approved artifacts and active work are never touched |

`specgate init --mode full` creates the deployment's `specgate.env` with mode `0600` and
repairs that mode when reusing an existing deployment. Keep this file local:
it contains the encryption key for stored settings. Use `--dir` with the local
stack commands when managing a non-default deployment directory.
Published Full initialization pulls the bundle's pinned appliance image before
startup so a same-tag image left in Docker's cache cannot be reused. The
contributor-only `dev` bundle continues to use its locally built image.

Full appliance `specgate init` accepts optional local-identity flags:
`--workspace-name`, `--display-name`, `--username`, and optional `--email`.
When `workspace-name`, `display-name`, and `username` are supplied together,
`init` bootstraps and saves that local identity after the appliance starts; if
`--seed` is also set, the demo seed uses the resulting workspace/user IDs for
attribution. If `--email` is also supplied, `init` forwards it to the same
bootstrap call. In `--no-input` mode, seeded installs must supply
`workspace-name`, `display-name`, and `username` up front or `init` fails
before deployment with an actionable required-flags error. Any partial
combination of those identity flags also fails validation before deployment.
Plain `specgate init --mode full --no-input` without identity flags remains a
stack-only install and skips local identity setup.

Mode-specific setup flags are strict. Local mode accepts `--local-dir` and
rejects the Full-only `--dir`, `--seed`, `--no-seed`, and `--bundle-version`
flags before creating state. Full mode accepts those appliance flags and
rejects `--local-dir`.

When `--install-plugins` is included, Local setup installs the selected Codex,
Claude Code, or Cursor package embedded in the CLI; use `--plugin-agent` in
non-interactive commands. Full setup downloads the selected plugin package from
that same Full appliance, including when `SPECGATE_PORT` uses a non-default
port.

Delivery `--init` scaffolds under the project `.specgate/` working directory;
the CLI creates/repairs that directory to mode `0700` and writes transient
completion and peer-review JSON files with mode `0600`. It refuses symlinked
working directories or scaffold paths, including with `--force`, so generating
a report cannot overwrite a file outside the selected project.

### Local CLI uninstall

In Local CLI mode, `specgate uninstall` removes CLI configuration and globally
installed managed plugin files while keeping the configured SQLite store.
Project-local plugin files in repositories are preserved and remain under
normal repository file ownership. `--purge-data --yes` prints a warning, then
deletes only SpecGate's `state.db` and SQLite journal files in that store;
other files in the selected real directory are preserved. Symlinked state
directories and database files are refused, non-regular database paths are
refused, and an existing database is restricted to mode `0600`. `--dir` is a
Full-only uninstall flag and is rejected in Local mode; Local uninstall always
uses the configured SQLite state directory. An empty `.specgate/` parent is removed; one containing
receipts or any other files is preserved. It does not invoke Docker. The interactive prompt
offers only IDE plugin files and Local SQLite state.

### Full appliance uninstall

In Full appliance mode, `specgate uninstall` removes user-local SpecGate setup.
In an interactive terminal it shows a checkbox list:

| Choice | Effect |
|---|---|
| IDE plugin files | Remove SpecGate files from Cursor, Codex, and Claude Code user plugin locations |
| Local data | Remove Docker volumes and the deployment directory |

By default, the command removes CLI config and IDE plugin files, stops a
CLI-managed Full appliance when present, and keeps artifact/spec data. Artifact
metadata, work items, evidence, settings, gate history, and artifact/spec file
contents share the `specgate-data` volume.

The public CLI installer and `specgate update` verify the selected release
archive against its published SHA-256 entry. Installation stops if neither
`sha256sum` nor `shasum` is available; verification is never skipped.

SpecGate writes ownership markers into appliance and IDE-plugin paths it creates.
Install/update refuses to overwrite an unmarked conflicting plugin path, and
Codex plugin installation also refuses a same-name marketplace entry that does
not point to SpecGate's managed plugin directory. Uninstall keeps unowned
same-name marketplace entries and removes only known managed files from marked
plugin paths. A native Codex or Claude Code installation makes CLI install stop
with exit code `4` before writing. Update and uninstall touch only marked CLI
files. Unknown files inside CLI-managed directories are preserved and listed as
`preserved_paths` in JSON output.
Symlinked deployment, cleanup,
plugin-target, and plugin-config paths are refused instead of followed. Full
data purge additionally requires the exact deployment marker and refuses
filesystem root, the user home, and a Git repository root.

For automation, pass `--purge-data --yes` only after backing up data you want to
keep. Plain/human output prints the exact deployment directory and destructive
scope before mutation:

```bash
specgate uninstall
specgate uninstall --purge-data --yes
```

`--purge-data --yes` removes:

- SpecGate-managed containers;
- SpecGate-managed Docker volumes;
- SpecGate-managed Docker networks;
- the deployment directory.

Release containers, volumes, and networks are labelled with
`org.specgate.managed=true` and `org.specgate.project=<project>`. Uninstall uses
both labels for cleanup so purging one Full appliance does not remove another.
The deployment directory must also contain SpecGate's exact private ownership
marker; matching filenames or Compose labels alone never authorize mutation.
Container images remain in Docker's cache for Docker or the user to prune.

### Advanced governance

| Family | Purpose |
|---|---|
| `policy` | List built-in governance levels |

For work-item policy, use `specgate work policy <ref>`.

## Interaction

When a work reference is optional, the CLI may show work needing attention and
ask you to choose. Full-mode confirmation prompts appear only in interactive
terminal sessions. Plain/JSON/no-input runs and piped stdin proceed without
prompting, so `--yes` is optional there. This applies to `gates run`, `delivery
review`, `delivery approve`, `delivery reject`, and `work archive`. Local mode
treats artifact approval, artifact promotion, and delivery approval or rejection
as explicit human decisions and requires explicit `--yes` in every output mode.

Automation should pass all required values and use `--json --no-input`.

## Token-conscious output

`specgate work context` returns the full Context Pack because coding agents need
the complete implementation contract. For full-route work it includes approved
artifact context; for quick work it is derived from the persisted title, intent,
and acceptance criteria. Other potentially large commands default to references
or summaries:

- `specgate artifact files <id> <path...>` returns file path, size, and URL when
  available. Add `--content` only when the file body is needed; it resolves the
  temporary file reference internally and emits content rather than a signed URL.
- `specgate gates check <artifact-id> --json --summary` runs readiness but omits
  evidence bodies, model metadata, and timestamps. It keeps the aggregate, gate
  states, hints, and dispatched IDE-agent task IDs. Use
  `specgate gates results <artifact-id> --json` to read the stored detailed
  evidence without running readiness again.
- `specgate artifact publish --file artifact.json --preview` expands local
  `source_file` entries and shows exact paths, roles, target, base version, and
  non-goals without network calls. After human confirmation, run the same
  command without `--preview`. Add `--compare <artifact-id>` only when a
  read-only comparison with one explicit published version is needed. That
  lookup is constrained to the package's explicit workspace or the selected
  workspace; it fails locally when neither is available. It reads
  artifact metadata and file hashes, never prior content, and reports added,
  removed, changed, and unchanged paths; it still performs no
  publication. `--compare` requires `--preview`. The publish command accepts local `source_file`
  paths or absolute local `file_url` entries in `documents[]`; every document
  must set exactly one of `content`, `source_file`, or `file_url`. The CLI reads
  those files and sends raw UTF-8 content, not base64, to avoid bloating IDE
  prompts. Unknown package or document fields are rejected instead of being
  silently ignored. A `source_file` must stay inside the package directory after symlink
  resolution, cannot itself be a symlink, and is limited to 1 MiB. Preview
  output shows its resolved absolute `source_path`; that local path is never
  sent to the server. Publish files use `request_type` for the work kind; omitted
  values default conservatively to the structured `unknown` value, without
  inferring intent from document prose. `work_type` is accepted as a CLI-only
  alias. `version` is server-assigned; use
  `base_version` only when publishing an update from an existing version.
- `specgate skill list --json` omits full prompts; add `--include-prompt` only
  when an agent needs the rubric body.
- `specgate delivery submit <ref> --file completion.json` replaces the
  report → gates → review → status command tail with one call and one combined
  `{report, gates, review, status}` envelope, so agents spend one round-trip
  instead of four. Scaffold the completion file with
  `specgate delivery report <ref> --init` — it prefills `criterion_id` and text
  for every acceptance criterion so agents do not have to re-derive them.
  `report` and `submit` add `criteria[].evidence.grounding` for cited local
  paths when evidence checking is enabled, giving delivery review a grounded
  trust-tier readback without sending your whole checkout.
  The command still emits the single combined result envelope when the
  authoritative verdict is `fail`, `needs_human_review`, or missing, but exits
  with code `1`; only `pass` exits `0`.
  Bare `--init` writes `.specgate/completion-<ref>.json` — a per-work-item
  scaffold under the gitignored repo-local `.specgate/` working directory (see
  [Configuration → Repo-level `.specgate/` directory](configuration.md#repo-level-specgate-directory)).
  JSON success returns the exact scaffold in `data.path`. If that regular file
  already exists, the command refuses to overwrite it and returns the same path
  in `error.details.path`; reuse it only after verifying its work and context.
  It also accepts an explicit `--init=<path>` or `--init <path>` to write a
  specific file elsewhere, creating missing parent directories privately; add
  `--force` to overwrite an existing scaffold.

## Compatibility

`specgate doctor` checks the active topology. In Local mode it verifies the
embedded store, selected user, and workspace without a server or TCP
connection. In Full mode it checks server and capability compatibility, then
prints setup state for identity, workspace source, model settings, Knowledge
embeddings, workspace membership, and IDE plugin follow-up.

For an unreachable Full server, run `specgate doctor` first. If an IDE agent is
sandboxed and cannot reach `localhost`, rerun that agent with local-network
access. Starting or repairing Docker services is an explicit user action; it is
not an automatic recovery step. Local mode never needs that recovery path.

```bash
specgate doctor --json
```

Agents use the successful JSON envelope's `data.mode` as the authoritative
topology signal: `local` is Local CLI and `full` is a Full appliance or
remote server. Do not infer it from a Docker process, a browser, or a configured
server URL. Only Full mode may use a returned `specgate open --print` URL; Local
mode hands off a stable ID and exact CLI command.

An exit code of `6` means the CLI and server disagree on a required contract.
Update the CLI or deployment before continuing.

`specgate doctor` reports Knowledge embeddings separately from chat/gate models.
If the embedding provider, model, or that provider's API key is missing, the
Embeddings line reports `missing` — Knowledge upload/search is unavailable and no
lexical fallback is presented as semantic search. Run `specgate model set` to
configure the embedding model.

When a workspace is selected, `doctor` also checks whether the current local user
is listed in that workspace's members. A `not_member` result means the claimed
local identity is not in `workspace_members`; it is a cooperative audit warning,
not an RBAC denial.

In human/plain output, `doctor` also shows a "Full appliance" section with the
running appliance service when a CLI-managed deployment exists. `local-status`
is the script-facing command for the same data. `specgate model test` verifies
that governance model provider/model/API-key settings are present; it is
settings-only and does not contact the model provider in the v0.1 CLI.

In Full mode, use `--fix` to repair common CLI-managed appliance failures:

```bash
specgate doctor --fix
```

In an interactive terminal, `doctor --fix` shows a checkbox list before changing
the local machine. The first repair action starts or repairs the CLI-managed
Docker appliance and then reruns the same doctor checks. For automation, combine
it with `--yes`:

```bash
specgate doctor --fix --yes
```

Local mode has no appliance to repair; use plain `specgate doctor` there.

For human/plain output, server-backed commands also warn when the connected
server recommends a different CLI version. Use the built-in updater:

```bash
specgate update
```

The updater installs the latest stable GitHub release, falling back to the
newest prerelease only when no stable release exists. An explicit `--version`
may select either. It refreshes only already-installed global Codex, Claude
Code, and Cursor plugins carrying SpecGate CLI ownership markers. Other plugin
installations stay untouched. In Local mode the updater then skips the appliance
step because no appliance belongs to that mode. In Full mode it
updates the Full appliance bundle and image when a CLI-managed deployment is
present. It does not create files for
an IDE the user did not select; refresh project-local installs with `specgate
plugins install --project-local`. Before appliance replacement it creates a
mode-`0600` recovery archive. Recovery archives remain until you remove them
explicitly with `specgate cleanup --backups`. A failed
target is rolled back automatically only when its release metadata says the
previous image remains compatible with any migrations already applied;
otherwise the command reports the recovery archive and does not restart the old
image.

On native Windows, `specgate update` downloads the matching release ZIP and
published checksum, verifies SHA-256, and replaces the current executable
without requiring `sh`.

If the connected server does not recommend a newer CLI, released CLI builds also
check GitHub releases in human/plain output and warn when a newer public release
exists. The GitHub check prefers the latest stable release and considers the
newest prerelease only before any stable release exists. It caches the result for 24 hours
under the user cache directory, never affects the command exit code, and is
disabled in `--json`, `CI=true`, `dev` builds, or when
`SPECGATE_NO_UPDATE_CHECK=1`.

IDE plugin setup can also be managed directly:

```bash
specgate plugins install
specgate plugins doctor
```

Before initialization, `plugins install` and `plugins doctor` use the package
embedded in the CLI so a clean IDE bootstrap does not require an appliance.
After Full initialization, `plugins doctor` compares installed files against
the configured SpecGate agent-package registry (the connected deployment by
default; pass `--registry` for another source). An explicit `--registry`,
`--server`, or `SPECGATE_SERVER` also selects the remote source before init.
Repository `.specgate/config` server values are never used for plugin packages
or hooks; this prevents a checkout from redirecting a global IDE install.
Before writing, install validates at most 16 safe skill names and preloads no
more than 32 MiB of required package files.
`plugins install --dry-run --json` returns the exact `planned_operations` while
keeping `written_count` at zero.
If a selected Codex or Claude Code target already belongs to its native plugin
manager, install stops before download or filesystem changes with exit code `4`.
JSON details include the owner, marketplace, removal action, and retry command.
If an official skills.sh-managed `specgate` bootstrap exists in project or
global scope, install stops before writing with exit code `4` and a `conflict`
error. JSON details list each bootstrap `scope`, `path`, and `remove_command`,
plus the exact `retry_command`. The CLI does not edit skills.sh files or locks.
`plugins doctor` reports native-manager ownership without fetching CLI package
inventory or inspecting manager caches. Malformed or ambiguous manager metadata
fails closed. For CLI installs, missing files still include an exact `specgate
plugins install ...` repair command; stale versions or a stale Codex plugin
cache warn with the reinstall/restart action to take. The check validates the
selected plugin files without requiring the corresponding IDE executable on
the current `PATH`.

Interactive `plugins install` and `plugins doctor` show a checkbox list for
Cursor, Codex, and Claude Code when `--agent` is omitted. The default selection
prefers locally detected IDE tools and falls back to all agents when none are
detected. Interactive `plugins install` also asks whether to write global user
files or project-local files. Scripts can still pass `--agent all`,
`--agent codex`, or a comma-separated subset.

Full appliance `specgate init` offers the same plugin install interactively,
including the IDE target checkbox list. Local automation can install any
embedded IDE target after init without a registry; for example:
`specgate plugins install --agent codex --project-local --no-input`.

Use `--project-local` on those commands only when the current repository should
vendor its IDE plugin files.

When multiple Full appliances exist, confirm or set the server URL first:
`specgate doctor`, `specgate local-status`, then
`specgate config server http://localhost:<port>/api/doc-registry` if needed.

## Related guides

- [Use the SpecGate CLI](../guides/cli-workflow.md)
- [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md)
- [Operate SpecGate](../guides/operate-specgate.md)
