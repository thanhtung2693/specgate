# CLI reference

The `specgate` CLI is the stable command surface for users, automation, and
coding IDE agents. It governs the handoff from source specs to reviewed,
verifiable delivery.

`specgate init` sets up Local mode by default. Run `specgate doctor` after setup,
then use `specgate change` for the governed delivery workflow.
`specgate ui` provides optional Local inspection
and human review. The CLI and coding agent cover the complete Local workflow
without a browser, menu bar, or OS notifications. Local needs no Docker,
model API key, or server.
Local `artifact publish --preview` resolves optional project instructions from
the selected Git root's `.specgate/rubrics/`. Its `policy.rubrics[].source`
and `digest` identify each effective rubric; custom previews also display them
in plain output. Publication uses the same resolution and freezes the resulting
text in its immutable policy. Missing files preserve the default output and
digest; invalid active files return validation exit `2` before publication.
See
[Local project rubrics](../guides/customize-governance.md#add-local-project-instructions).

For exact flags in your installed version:

```bash
specgate <command> --help
```

Commands reading a JSON request file accept at most 64 MiB of encoded input.
This permits the 10 MiB artifact package content limit even with six-byte JSON
escapes, plus metadata overhead. Oversized input returns a usage error (exit
`2`) before parsing or submitting the request. Command-specific
content and validation limits still apply. Regular-file symlinks remain
supported; the size limit also applies while reading if a file grows.

## Local resume and verification contracts

`work resume <ref> --json` is a read-only Local pickup packet: complete work
description and acceptance criteria, the approved document index and digests,
verification contract, and current change status (including the next actor,
missing evidence and next command). A reference is mandatory; no branch-name or
most-recent-work inference occurs. Different work items on one spec remain
separate. Receipt freshness is checked when a receipt exists.

### Advanced Local

Add these controls when the work needs stronger verification or an explicit
comparison. The ordinary workflow can leave the verification contract
`unconfigured`.

- A verification pin fixes the reviewed command and working directory for
  each bound check. Preview and confirm it before the first report.
- A pinned check can observe specific JUnit cases and changes at watched
  inputs when the CLI reruns the check.
- A checkpoint saves a baseline to compare when resuming or reviewing work.
- Source lineage records how named requirements changed between two exact
  artifact versions. Use `artifact impact` to inspect that declared pair.

See [compatibility limits](feature-status.md#established-paths) before the first
enhanced write. The CLI discloses the store compatibility upgrade and backup
before applying it.

`work checkpoint <ref>` records an explicit, append-only Local pickup baseline;
in unattended use, run `specgate --yes work checkpoint <ref> --note "..."`.
`work resume <ref> --since <checkpoint-id>` shows the exact baseline and its
pickup note. It reports a file-content delta when the checkout is comparable,
without changing scope, the checkout, or the stored work item. Checkpoints record HEAD/tree and bounded
dirty-file hashes; a file becoming clean is not treated as deleted. Each
checkpoint also retains the exact latest completion/review IDs when present;
later submissions do not rewrite those historical IDs. A different
checkout or unavailable Git state is explicitly noncomparable. If Git state or
the bounded dirty manifest cannot be captured, checkpoint creation fails
without storing an unusable baseline. A dirty file replaced by a directory is
recorded as a removal plus additions for its untracked children. An unknown or
foreign explicit checkpoint is a conflict, never a silent fallback.
Omitting `--since` selects only the newest checkpoint for that work in the
current checkout and Local store generation. On a received store, use
`--since` to read a source checkpoint and its pickup note. Its delta is
`noncomparable` even when the checkout path and Git HEAD still match. Without one, it can use a comparable shared-repository
completion receipt to list committed Git-tree changes, explicitly marked as
limited because it has no historical dirty-file manifest; otherwise it reports
`no_baseline`. It never silently uses a checkpoint from another worktree; only
an explicitly selected checkpoint on `change status`/decision commands enters
an acceptance basis.
Large resume deltas return added/removed/modified totals and `truncated: true`
by default; rerun the same read-only command with `--detail` for every path.
Plain output also shows the counts, available path lists, and comparison limits.
Paths are repository-root-relative even when invoked from a subdirectory.
A committed rename appears as removal of the old path and addition of the new
path; this comparison does not infer rename identity.
If a newer Local artifact version exists, resume names the latest one as an
informational reference; the work's pinned version, criteria, and Context Pack
remain unchanged, even after the newer version is approved and promoted.
New work created from that feature uses its newly promoted canonical version;
existing work is not retargeted. Document paths are resolved within the selected
work's snapshot, so different features can use the same `spec.md` or `plan.md`
without sharing document bodies.
Checkpoint and resume JSON retain only `dirty_file_count` and
`changed_file_count`; the bounded path hashes remain in the Local store for
comparison and are not echoed into agent context.

When `change status` returns `basis_digest`, copy its exact `review_id` and
`basis_digest` into `change accept` or `change request-changes`. This binds the
human decision to the current enhanced verification/checkpoint material. Older
Local work without either enhancement keeps the existing `--review-id` flow.

`artifact impact <target-artifact-id> --compare <base-artifact-id>` is a
read-only Local exact-pair inspection. Both artifacts must belong to the same
feature; a cross-feature pair is rejected with validation exit `2`, not an
availability error. It reports declared requirement
transitions only when the target lineage names that exact base artifact and
snapshot digest; reverse, nonadjacent, or undeclared pairs remain explicitly
unavailable rather than being inferred from matching text or IDs. For a
declared pair it also shows immutable base/target status and digests, document
add/remove/modify identities, before/after recorded requirement fields and
literal changed-field flags, explicit additions/removals/deferrals and target
additions without linked work. It never guesses a transitive mapping.
When publishing lineage JSON, `source_lineage.rows[].target_ids` and
`source_lineage.added`, if supplied, must be arrays containing only strings;
malformed entries are rejected rather than dropped.
Lineage requires a known base requirement inventory: recorded source criteria
or an earlier declared lineage. Empty inventory on an older artifact is
unknown, not proof that the earlier spec had no requirements, so SpecGate will
not accept a new lineage declaration from that base or report its impact as
declared.
Its JSON projection separately lists exact base-version and target-version work
with their explicit `@source` links, phase, latest review/evidence state, and
unlinked work that needs inspection. These are recorded associations—not a
claim that other source text is unaffected or that two work items are safe to
run in parallel. When declared lineage maps open work to the same exact target
source ID, `source_overlaps` reports a bounded coordination hint. `path_overlaps`
reports shared paths from the latest affected-file lists only when completion
receipts establish the same repository or local checkout; it includes work and
report IDs. When no comparable overlaps are recorded, missing lists yield
`unknown`, and differing repositories yield
`noncomparable`, not a path match. Reported paths can be stale or incomplete;
`no_recorded_overlap` is never a conflict-freedom guarantee. Plain output shows
these facts as well as JSON.
`recorded` means some comparable overlap hints exist, not that every open work
has a usable report. Inspect the individual work/report identities and path
lists for missing or foreign-repository inputs even when hints are present.

Local artifact packages can add explicit source requirements:

```json
"source_criteria": [
  {"id":"req-id","text":"Record has a stable ID","source_path":"spec.md"},
  {"id":"req-later","text":"Export CSV","source_path":"spec.md","deferred_reason":"Out of scope"}
]
```

Each `source_path` must name a document in that artifact package. IDs must
start with an ASCII letter or digit, then contain only ASCII letters, digits,
hyphens, or underscores, so a work acceptance criterion can map them reliably
with `@source:req-id`. Unknown IDs are rejected when creating Local work with a
validation error (exit `2`), before writing any work item. Correct the mapping
against the approved artifact's source inventory before retrying. `coverage`
and `artifact coverage` show `source_coverage` alongside the existing
work-delivery `state`; that state is unchanged by source coverage.

In Local JSON, `coverage` returns `source_requirements` for every canonical
artifact, including an empty array when that artifact has no inventory.
`artifact coverage` returns the same rows for its exact artifact version. A row
has its stored `id`, `text`, and `source_path`; a `state` of `unassigned`,
`in_progress`, `delivered`, or `deferred`; optional `deferred_reason`; and the
linked work items. `deferred` takes precedence even if work is linked. Linked
work is matched only through exact `@source:<id>` tokens on that artifact
version. `source_next_action`, when present, names the read-only artifact command
to inspect before proposing missing work.
Local-only rows.

Plain `coverage` lists unassigned and deferred entries immediately. Plain
`artifact coverage` lists every entry and its linked work. A delivered work
state with unassigned source requirements means the created work was delivered;
it does not mean the reviewed inventory is fully assigned. An inventory is only
as complete as the human-reviewed list: SpecGate validates its stored IDs and
links, not whether an agent extracted every requirement from the source.

Source-criterion fields must be strings; omit `deferred_reason` when the
requirement is not deferred. Local `artifact publish --preview` validates the
inventory and shows each requirement's text, ID, source path, and any deferral
reason in both JSON and plain output.

`work context <ref> --summary --json` returns scope and the pinned document
index without document bodies. Read a specific indexed snapshot with
`work context <ref> --document "specs/design.md" --json`. If one path has
multiple indexed roles, it refuses the ambiguous read; pass `--role <role>`
from the index. These Local-only flags
are mutually exclusive. The summary is an index, not permission to skip
non-goals or other constraints in the indexed documents. Existing unflagged
Context Packs and their digests are unchanged. Document reads use stored
approved content, not the current checkout or a newer artifact version.

For automated criteria, optionally pin reviewed check commands before the
first completion report:

```bash
specgate work verification LOCAL-123 --json
specgate work verification LOCAL-123 --file checks.json --dry-run --json
specgate --yes work verification LOCAL-123 --file checks.json --json
```

The input is:

```json
{
  "context_digest": "<digest from work context>",
  "shell": "sh",
  "checks": [{"name": "unit", "command": "go test ./...", "cwd": "."}]
}
```

For a stronger Local-only pin, a check can declare `test_report` with format
`junit` and exact `(classname, name)` selectors keyed by the persisted Local
criterion IDs, plus explicit regular `watched_paths`. A report-enabled check
must be submitted with `--run-checks`; SpecGate supplies a fresh,
ignored `SPECGATE_TEST_REPORT` path, reads at most 16 MiB / 100,000 cases, and
stores normalized selected-case outcomes rather than XML or command output.
It does not infer tests, install a runner, or prove a runner did not fabricate
the report. Changed watched bytes are a visible review warning, not a pass.
For a runnable configuration and a missing-test/rerun example, follow
[Observe selected tests](../guides/observe-selected-tests.md).

Any failure/error in the supported report fails the check, including unselected
cases. Selected skipped, missing or duplicate cases cannot pass. Unsupported
XML roots, retry outcome extensions and ambiguous outcomes are rejected. Several
criteria may deliberately share one exact selector.

The report directory must be Git-ignored; a symlinked `.specgate` directory is
refused. Cleanup removes only the allocated report file and an empty run
directory, leaving unrelated runner-created files alone. Watched inputs must
resolve inside the checkout and are read with a 16 MiB per-file bound. Missing
and unreadable inputs remain visible instead of being reported unchanged.

Report-enabled executions store a versioned run receipt bound to the work,
context, pin, check and completion. It records exit status and before/after
checkout and watched-input observations. Changes to Git or watched-file
states/digests between endpoints fail the check, including Git-ignored watched
files. Equal watched endpoints that already differ from the pin retain a
separate drift warning;
unavailable observations are not proof of freshness. Endpoint observations
cannot detect edits reverted between observations or authenticate the runner.

`change status`, `change accept`/`request-changes` and `delivery approve`/`reject`
accept explicit `--checkpoint <id>` and paired `--impact-base <id>` /
`--impact-target <id>` selections. Repeat the exact selections when deciding.
Unselected comparisons are `not_requested`, not implicitly the newest record.
An acceptance impact pair must include the work's lead artifact. Quick work
has no lead artifact and rejects impact selections; inspect unrelated pairs
separately with `artifact impact`.
Selected checkpoints include a current-versus-checkpoint path comparison in
`selected_checkpoint_comparison` and the acceptance basis. Status reports
counts by default; `--detail` includes every compared path. Acceptance selections
require a checkpoint from the same work, workspace, checkout and Local store
generation. Unavailable comparisons fail before a basis can include them.
Use `work resume --since <id>` to inspect an unavailable comparison without
making it an acceptance input.

Each name must match an existing `@check:<name>` acceptance-criterion binding;
every bound name must occur exactly once. Commands must be nonempty. `cwd`
defaults to `.`, is relative to the Git checkout root, and must be an existing
directory inside it (symlink escapes are rejected). Pin from the target
repository. Only `sh` is supported; commands are not sandboxed.

The contract is immutable per work and bound to its Context Pack digest.
Repinning, pinning after a report, or pinning delivered work is a conflict. To
change the contract, create replacement work. Dry-run validates without
pinning; a real pin requires confirmation (`--yes` in automation). Human previews
and confirmation show commands/cwd, exact criterion-to-testcase selectors, and
watched paths with their pinned hashes, just as JSON previews do.
Watched bytes are hashed again after confirmation and before persistence;
changes from the preview are a conflict requiring another review.

The first v2 pin, checkpoint or lineage artifact upgrades the **entire Local
store**, not just that work item. The confirmation names the exact
`state.db.pre-enhanced.bak` backup path and warns that older CLIs cannot write
the upgraded store. Unattended first upgrades require `--yes`; JSON receipts
(and verification dry-run output) include `store_upgrade`. Restoring the backup
returns to pre-upgrade data only; it does not preserve later changes. Keep using
a supporting CLI for the live upgraded store.
Delivery-handoff export destinations cannot overwrite this backup, including
through symlink or hard-link aliases from another workspace. Handoff/v1
refuses work carrying Local verification pins, checkpoints, lineage, or
acceptance bases. A complete Local backup preserves those records.

`delivery report <ref> --init` copies pinned commands/cwd and
`verification_contract_digest`. Submit rejects changed, duplicate, extra, or
missing checks before execution and checks the contract again when persisting.
`--run-checks` executes pinned commands relative to the checkout root even from
a subdirectory. Without a pin, legacy command execution is unchanged.

Status, resume and acceptance output distinguish `pinned` from `unconfigured`.
Pinning is not a test pass: reported checks remain agent-reported unless
SpecGate reruns them. Failed/skipped checks remain visible; a human decision
does not erase those gaps. This prevents accidental command substitution
through the CLI, not weak test design or tampering by someone who controls the
Local database. Local user names are cooperative attribution, not proof that a
human operated the CLI.

## Local snapshot transfer

These advanced Local commands support sequential machine handoff for one
person. The archive contains the **entire selected Local store** with private
history and committed WAL data.

| Command | Effect |
| --- | --- |
| `specgate transfer prepare --work <ref> --file <archive> [--checkpoint <id>] [--note <text>]` | Stage once, show its inventory and pickup, and publish the confirmed snapshot to a new file. Current project/workspace rules apply. Use `--yes` without prompts. |
| `specgate transfer inspect --file <archive>` | Verify the ZIP, manifest, checksum and SQLite snapshot with no active store or workspace. Works even when unrelated config is malformed. |
| `specgate transfer receive --file <archive> --into <new-directory> [--expect-snapshot <sha>]` | Restore only to a new directory under an existing parent and assign a fresh checkout generation. Interactive use confirms the verified scope and hash. Unattended use requires the exact archive SHA via `--expect-snapshot`. |
| `specgate transfer activate --from <received-directory> --repo <local-git-root>` | Preview and change the binding of one Git root after Git provenance and bookmark checks. Use `--yes` without prompts. Follow the returned scoped `work resume` command. |

Prepare and inspect return `snapshot_sha256` and `manifest` in `--json`.
Receive also returns `receiving_generation`, `directory` and
`activated: false`. Activation reports `old_binding`, `new_binding` and
`resume_command`, including `--since` if you selected a checkpoint. Use the
returned `command_shell` (PowerShell on Windows or a POSIX shell elsewhere). A
post-save verification error includes `activation_committed: true`,
`verified: false`, both paths and a scoped recovery command in
`error.details`. Inspect those details before attempting another change.

The v1 ZIP has only `manifest.json` (1 MiB maximum) and `state.db`
(512 MiB maximum), with an overall archive limit of 520 MiB. The archive
SHA covers both entries and the payload SHA checks the SQLite bytes. Keep
the archive private. The checks provide consistency, not authentication.
Existing destinations or SHA/binding conflicts exit 4, and unsupported
formats or store versions exit 6. Receive requires a destination filesystem
that supports an exclusive directory rename. Surrounding whitespace in
`--into` or `--from` is a usage error.

Activation requires a matching normalized Git origin when the source has
one. Use the same origin transport and path on both checkouts. A source
without a usable origin requires its exact commit and tree in the target
checkout. This establishes repository identity for the binding.
Current code and validation still have their own freshness checks. Stop
source writers before switching. See [moving a Local store](../guides/operate-specgate.md#move-a-local-store-to-another-checkout).

## Local setup diagnostics

Local `doctor` checks both global plugins and the current repository's root
plugin files, even from a subdirectory. Diagnostics name each detected scope
and return matching verification commands; they do not prove IDE session
loading. A stale repository binding returns an explicit `workspace bind <slug>`
for the durable Local selection, so recovery does not reuse the broken binding.

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
disables interactive prompts. Prompts require terminal input and terminal
stderr outside CI. They always write to stderr, including in accessible mode.
Without prompts, supply explicit flags or a file body when a command needs
input.

Bare command groups such as `specgate --json` and `specgate work --json`
return one success envelope with group help in `data.help`. Explicit
`--help`, the `help` command, and `--version` retain their normal text.
A valid `completion <shell>` invocation emits a raw completion script, even
with `--json`. Use `specgate version --json` for the version envelope.

Invalid flags and positional arguments return a `usage` error with exit code
`2`; in `--json` mode both use the same JSON error envelope as command
validation failures and include the relevant `--help` command.

Other global flags:

- `--version` — print the installed CLI version;
- `--workspace <slug-or-id>` — use a workspace for this command only;
- `--yes` — accept confirmations;
- `--no-input` — fail instead of prompting;
- `--timeout <duration>` — request timeout for release downloads (default 3 minutes; non-positive values use the
  default);
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

JSON mode does not mix prose into stdout, and every invocation emits exactly one
envelope. An operational error carries an `error.code` and a message naming what to
do, including a missing argument or a prompt that cannot run under `--no-input`;
a nonzero exit with empty output is a bug, not a shape callers must handle.

A computed governance verdict is different from an operational error. For
example, `change submit` can return `ok: true` with its stored review in `data`
and exit `1` when delivery requirements fail. Automation must inspect the exit
code and verdict, not treat `ok: true` as evidence that delivery passed or was
accepted.

## Exit codes

| Code | Meaning |
|---:|---|
| `0` | success |
| `1` | governance requirement failed |
| `2` | usage or validation error |
| `3` | resource not found |
| `4` | conflict or stale state |
| `5` | service unavailable |
| `6` | incompatible configuration, store, or operation |

## Command lanes

Run `specgate --help` for the command groups. **Start here** lists setup and
the normal delivery loop. **Optional browser review** lists `ui` and `open`.
Use **Advanced and maintenance commands** for evidence inspection,
preparation, and maintenance. Every listed family has direct help with
`specgate <command> --help`.

### Start here

| Family | Purpose |
|---|---|
| `init`, `doctor`, `plugins` | Set up the CLI and agent and check the selected installation. See [Setup and identity](#setup-and-identity). |
| `change` | The normal approval and post-handoff facade. Use the dedicated [Change facade](#change-facade) reference for exact syntax, output fields, and confirmation behavior. |
| `status` | Show selected-workspace board counts, attention count, and next action. |
| `work` | List the attention queue or ready work in the selected workspace with `work list --phase ready`. Inspect one item with `work show <ref>`; use `work resume <ref>` for an agent pickup packet. Quick work requires one or more explicit `--ac` values. |

### Advanced workflow and diagnostics

| Family | Purpose |
|---|---|
| `capabilities` | Print the machine-readable Local capability manifest. |
| `coverage` | Classify every canonical specification in the selected workspace as `uncovered`, `unfinished`, `stale`, or `delivered`, including the governing artifact, related work refs, and a next command for every non-delivered state |
| `verify` | Produce one read-only closeout verdict for a work ref: artifact-backed work includes the exact artifact version and digest, while artifact-free work sets `quick_route=true`; both include criterion evidence, checks, authoritative delivery verdict, cleanup eligibility, and the next command. A non-closeable result exits `1` |
| `audit` | Read a work item’s chronological governance history. Read readiness results for artifacts with no work item via `gates results <artifact-id>`. |
| `feature` | `feature list` lists governed features. Use `feature show <key-or-id>` to check the exact feature key before publishing a new version. |
| `artifact` | Preview and publish immutable artifact versions. Use `artifact list` and `artifact show <id>` to inspect a version before approval or promotion. `artifact coverage <artifact-id>` reports exact-version delivery. Read pinned contents with `work context <ref> --document <path> --role <role>`. |
| `gates` | Artifact readiness checks (`check`), stored artifact results (`results`), and IDE-agent artifact gate tasks (`tasks`) use the `check` → `tasks list/show/submit-result` → `results` loop. Each frozen task is submitted separately so its immutable digest and result remain independently attributable; results retain independent digests. Compact `gates check --summary` output preserves each result's executor origin so agent-attested evidence is not presented as platform evaluation. Human `gates check` output names every gate with its state, trust origin, and hint beneath the aggregate, because an aggregate alone cannot distinguish one soft hint from four gates still awaiting a result. `gates check` dispatches tasks as part of the readiness run; `gates tasks dispatch <artifact-id>` redispatches them alone when the artifact is unchanged. `tasks list` already returns each task's `skill_content` and both digests, so `tasks show` is only needed for a task id obtained some other way. A rejected `submit-result` names the field that did not match the dispatched task — `gate`, `gate_digest`, `input_digest`, `evaluator.executor`, or `state` — with the value the task expects. A task rejected as stale means the artifact or its policy was republished after the task was frozen; re-dispatch with `gates check` and judge the fresh task rather than resubmitting. |
| `delivery` | Report implementation, review that evidence, and decide delivery as a human reviewer. See [Delivery commands](#delivery-commands) for every subcommand, the completion scaffold contract, the assurance rules, and Git receipt freshness |

## Change facade

`change` is the normal view over existing artifact, work, and delivery records;
it does not create a new durable Change entity. Its exact commands are:

```bash
specgate change status <work-ref>
specgate --yes change approve <artifact-id> --title <title> --ac <criterion> [--ac <criterion>...] [--description <text>] [--note <note>]
specgate change submit <ref> [--file <completion.json>] [--run-checks] [--skip-evidence-check]

# Local: explicit human assertion is required.
specgate --yes change accept <ref> --review-id <review-id> [--note <note>]
specgate --yes change request-changes <ref> --review-id <review-id> [--note <note>]

```

Local decisions require `--review-id` from the status you reviewed, including
the expert `delivery approve|reject` commands. Status prints a complete command.
If a newer report arrived, the decision exits `4` (conflict); inspect the new
evidence before deciding again.

`work list --phase` accepts comma-separated phases, ignoring case and
surrounding whitespace. Select each Local workspace explicitly when inspecting
its work.
Feature-backed Local work keeps its `feature_key` in list/show and
summary/resume JSON after creation; quick work has no feature association.
These reads retain the exact pinned artifact and Context Pack digest.

For `change submit`, a file-safe `<ref>` defaults to
`.specgate/completion-<safe-ref>.json`. A file-safe ref contains only letters, digits, `-`, and `_`. An unsafe ref must pass `--file` with a completion file path.
Use the default submission form for normal work and the explicit file only when
the ref is unsafe or the completion file is intentionally elsewhere:

```bash
specgate change submit CR-123
```

`--run-checks` re-executes non-skipped checks through `sh -c` after previewing
their commands; a scaffolded `pending` row is accepted when it has a runnable
command, then replaced with the observed `pass` or `fail`. Noninteractive
callers must also pass `--yes`. Use
non-interactive POSIX shell commands. `--skip-evidence-check` bypasses
local citation validation and should be reserved for recovery when the cited
paths cannot exist in the current checkout.
Check names and statuses ignore surrounding whitespace consistently; a padded
`skipped` status still skips execution. Read projections preserve the original
historical report.

`change approve` is the normal feature-backed preparation decision in Local. It records approval of the exact artifact snapshot, promotes that
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
`change submit --json` returns the compact actionable Change status.
`delivery submit --json` returns the delivery review; use `change status --detail`
to inspect criterion evidence and the current next action.

If `change submit` needs a retry after reading an explicit `--file`, its
immediate `next_command` includes that exact path, quoted for a POSIX shell.
Later status reads remember whether the submission used a non-default file
without storing its path. If `next_command` ends in `--file`, append the path
to the revised report from the same work. The normal default-file retry stays
runnable as shown.

`change status` gives the next safe action. Human output names the same concepts
as the JSON fields below; JSON emits the standard success envelope with this
object in `data`. Default Local output keeps each criterion's verdict and Why
beside its claim, bound check status, check provenance, citation grounding, and
recorded gaps. `agent_attested` means agent-reported; `command_only` means the
CLI observed the command without selected testcase observations;
`selected_test_observed` means the CLI observed the pinned selected testcases.
Missing legacy provenance stays `unknown`. `change status <ref> --detail` adds
evidence paths, citation anchors, selected-test outcomes, and comparison details
to the same criterion block. `work resume` uses the same default status display.
`change submit`, `change status`, and `work resume` show the stored completion
summary when present. JSON preserves it verbatim, and `work resume --json`
includes it inside `data.status`. Plain output frames each line beneath
**Agent-reported summary** and neutralizes terminal control characters. The
summary is reported context for the reviewer.
Checkout freshness and human decisions remain separate from historical check
outcomes; acceptance does not erase recorded gaps.

| JSON field | Meaning |
|---|---|
| `mode` | `local`, the mode used to derive the status and command. |
| `ref` | Resolved work reference used by follow-up commands. |
| `title` | Existing work item's title. |
| `state` | One of the action states described below. |
| `evidence` | Summary of delivery evidence; it is not a human decision. |
| `assurance` | Summary of the review/assurance source; it is not proof that a human accepted delivery. |
| `decision` | Recorded human delivery decision, if any. |
| `receipt` | Recorded Git receipt summary, if any. |
| `freshness` | Whether the stored evidence was checked against the current checkout. |
| `verification_contract` | Local verification setup: `pinned` means the exact criterion-bound commands and directories were fixed before reporting; `unconfigured` means checks remain self-selected. A pin does not itself prove a check passed. |
| `verification_version`, `verification_digest` | Version and digest of the exact Local verification contract, when pinned. |
| `artifact_id`, `artifact_version`, `artifact_digest` | Exact approved Local artifact snapshot for this work; absent on the quick route. A newer artifact is informational and never replaces this scope automatically. |
| `context_digest` | Exact Local Context Pack digest for this work. |
| `completion_id` | Exact Local delivery report ID associated with `review_id`. |
| `completion_summary` | Optional agent-reported prose from that completion, preserved verbatim. |
| `peer_state` | Peer-review state associated with the current completion. |
| `criterion_evidence` | Per-criterion claim, bound check status and provenance (`selected_test_observed`, `command_only`, or `agent_attested`), selected testcase outcomes and report digest, citation grounding, verification-run freshness, and explicit gaps. Missing facts remain missing, not passing. `criteria[].why` also includes a CLI-observed selected-test report diagnostic when a bound Local check fails because its required case is missing or invalid. |
| `source_coverage`, `source_requirements` | Exact artifact-version source inventory coverage, including unassigned or deferred requirements. These are separate from this work's criterion verdict. |
| `selected_checkpoint`, `selected_checkpoint_comparison`, `selected_impact` | Optional, explicitly requested Local checkpoint or artifact-pair comparison. The checkpoint comparison reports changed-path counts by default and bounded path lists with `--detail`; checkpoint output excludes the captured file contents. |
| `risks` | Local counts and affected criterion IDs for failed, missing, stale, and unknown observations. Counts are recorded issues, not a confidence score; source requirements outside this work are separate. |
| `next_actor` | Actor expected to take the next action: `implementing_agent`, `human_reviewer`, `maintainer`, or `none`. |
| `missing` | Always an array of named requirements still missing; empty after acceptance. |
| `guidance` | Optional human rework note, retained across undecided retries until a later human decision replaces or clears it. |
| `guidance_review_id` | Review that recorded the human guidance; it may precede the current `review_id`. Present only with guidance. |
| `stale` | Whether peer-review evidence is stale or the current checkout differs from the stored Git receipt. |
| `stale_reason` | Optional; omitted unless `stale` is true. |
| `next_command` | Next CLI command for the returned state and mode. When a retry ends in `--file`, supply the revised completion file path. |
| `review_id` | Local delivery review shown by this payload; pass it as `--review-id` when accepting or requesting changes. Omitted before a report exists. |
| `basis_digest` | Present when the decision requires an exact acceptance basis, including reviews in a received Local store. Pass the current value as `--basis-digest`. |
| `historical_verification` | The recorded completion comes from another Local store generation. Its check outcomes and provenance remain available as history. |
| `acceptance_basis` | Current enhanced review material, including watched-input observations and explicit comparison selections. A changed material digest requires another review before deciding. |
| `recorded_acceptance_basis` | Immutable basis stored with the human decision, including actor, note and observation time; omitted for legacy decisions. This historical record is distinct from the current checkout observation. |

Criterion risk categories use the recorded check, selected-case, grounding,
and freshness outcomes. Words inside a check name, testcase name, or evidence
path cannot turn missing evidence into a failed or stale observation.

`evidence`, `assurance`, `decision`, and `receipt` are separate summary labels:
evidence says what was reported or reviewed, assurance says how it was assessed,
decision says what the human recorded, and receipt identifies the stored Git
provenance. Do not treat one as authority for another.
The expert `delivery status --json` readback likewise keeps authoritative
`verdict` separate from optional `evidence_verdict` on a human decision.
| `state` | Action semantics |
|---|---|
| `implementation` | An implementing agent must add or repair delivery evidence. With no report, `next_command` creates the scaffold; after a failed review, it resubmits the existing completion with locally reproduced checks. |
| `awaiting_review` | Evidence needs human interpretation; `next_actor` is the human reviewer and `next_command` opens the detailed criterion review, which ends with the exact accept and request-changes commands. |
| `review_pending` | A newer completion has not received its own delivery review; the implementing agent runs the returned review command before any human decision. |
| `awaiting_acceptance` | Evidence passed but a human must decide. The next command is `specgate --yes change accept <ref> --review-id <review-id>`. |
| `accepted` | A human accepted the current reviewed completion. The evidence label still discloses an advisory or false-negative assessment; no actor is pending and the next command is the audit trail. |
| `rework_requested` | A human requested rework. Follow `guidance`, revise the existing completion, then run `next_command` to submit it for a new review cycle. |
| `blocked` | A maintainer must restore delivery policy; the next command opens detailed delivery status. |

`stale` does not replace the delivery decision: it flags stale peer-review
evidence and explains why in optional `stale_reason`. Follow `next_command`,
and use `missing` and `next_actor` to decide whether to implement, review, or
escalate. Acceptance and rework require `--yes`.

For detailed preparation or troubleshooting, keep using the expert `delivery`,
`work`, `gates`, `artifact`, `audit`, and `verify` families. `change prepare` is
not available: agents still prepare and preview the explicit artifact and work
contract before the human runs `change approve`.

`work create --feature <key-or-id> --title <t>` creates a feature-backed work
item bound to the feature's approved canonical artifact (the lead artifact is
set automatically; the feature must have a promoted canonical). The IDE agent
reads every explicitly mapped source spec document and submits each confirmed
criterion with a repeated `--ac`. SpecGate requires a non-empty explicit list
and does not infer criteria from headings, numbering, filenames, or keywords.

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
after the work has an explicit human delivery approval in the Local store. An
advisory or false-negative evidence verdict remains visible but does not erase
that human authority.
When a review exists but closeout is unresolved, `verify` directs you to
`specgate change status <work-ref> --json` for the current actor and exact
decision command, including any required review ID and acceptance basis.
An explicit `--workspace` override is retained in that follow-up command.

### Hand a review to a teammate through the repository

A delivery handoff is an unsigned, committable review request. The author
exports one file after the review runs. A reviewer renders it beside the diff
without a workspace or Local state:

```bash
# Author, after `change submit`:
specgate delivery handoff export <work-ref>
git add .specgate/handoffs/<work-ref>.json && git commit -m "review request"

# Reviewer, on the same branch:
specgate delivery handoff show --file .specgate/handoffs/<work-ref>.json
```

`export` is Local-mode only and refuses a work item with no delivery review,
pointing at `delivery report --init` instead. It writes to
`.specgate/handoffs/<work-ref>.json` unless `--file` names another path, and
warns when Git ignores the destination, because a reviewer would never receive
it. SpecGate refreshes its exact generated `.specgate/.gitignore` from older
releases so handoffs remain committable; a user-edited ignore file is preserved.

The bundle carries the work item, its review, the bound completion, and
peer-review status. The export drops grounding excerpts and keeps the recorded
status and digest. The reader can inspect that record. The bundle does not
independently verify the cited source. The active
Local SQLite file and its journal files are refused as destinations, including
symlink and hardlink aliases and the default output path. Export refuses a
symlink or non-regular destination file and a symlinked default handoff
directory. It atomically replaces a regular destination with a private
mode-`0600` bundle; replacing an existing file does not modify its other
hardlinks.

`show` is read-only and needs no workspace. It rejects a bundle whose checksum no longer matches its contents,
re-derives the verdict from the evidence the bundle carries rather than trusting
the recorded one, and reports a disagreement between the two — which is how a
bundle exported by an older CLI, under weaker binding enforcement, is caught. It
exits `1` when the re-derived verdict is not `passed` or the two disagree.

Reviewer decisions do not travel back in the file. The author opens
`specgate change status <work-ref> --json` in the owning Local store and follows
its current next action. Displayed verification observations come from the
source report.

For IDE gate tasks, run:

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
receipt; `created_task_ids` only records what that invocation
created and may be empty on an idempotent repeat.

In Local mode, explicit `gates tasks dispatch` refreshes a completed
`spec_repo_drift` check even for an unchanged artifact: the repository may have
changed since its earlier result. It reuses an unexpired pending drift task and
completed document-only checks. Readiness reads use the newest drift task, not
an older passing result. Dispatch does not snapshot the checkout; the IDE agent
must examine the current repository when completing the task.

`work create-quick <title>` uses the title as the description when
`--description` is omitted, so the shortest quickstart form still satisfies the
contract. Repeat `--ac` on `work create-quick` or feature-backed
`work create` to supply human-authored acceptance criteria.
In Local JSON input, every `acceptance_criteria` row must be a string or an
object with string `text` and an optional string `verification_binding`.
Malformed rows reject the entire request with validation exit `2` before any
work is created; they are never silently removed or converted into unbound
criteria.
Append a trailing `@check:<name>` token to bind that criterion to a delivery
report check, for example
`--ac "POST /imports rejects bad CSV rows @check:integration"`. The check name
must match a later `delivery report` / `delivery submit` `checks[].name`. A
bound criterion requires an evidence-backed satisfied claim and a passing
bound check. Do not invent bindings on behalf of the human.

Every command that establishes acceptance criteria reports how many of them a
check will decide: `work create-quick`, `work create`, and `change approve`. The human output carries a line such as `Enforcement: 1 of 2
criteria are bound to a check; the rest are graded on the agent's claim.`, and
`change approve` states the same counts in its confirmation prompt, while the
list can still change. `work create-quick` and `change approve` also return
`acceptance_count` and `acceptance_criteria_bound` in their JSON envelopes.

The count is of resolvable bindings, not a judgement about criterion wording; an
ambiguous binding counts as unbound, because review falls back to the agent's
claim for it. Quick work never runs the artifact route's
`acceptance_criteria_verifiable` gate, so for quick work this count is the only
enforcement signal available at creation time.

The reader shows the claim and the outcome of its named check. Binding comes
from the stored acceptance criterion even when the completion body omits
`verification_binding`. Local review fails when the bound check failed, was
skipped, or is absent from `checks[]`, and names the criterion
and check. Enforcement covers a missing or
skipped check, not a falsely claimed `pass` — `checks[].status` remains
agent-reported unless the submission ran with `--run-checks`.

`specgate artifact approve` reports governance-policy refusals with exit code
`1`, including Local approval while readiness gates remain unresolved or fail.
Local `specgate artifact promote` also returns `1` when the artifact is not yet
approved, rather than reporting the store as unavailable.
Complete the required gate reviews before retrying. Artifact approval
requires an explicit human decision and records cooperative attribution.

## Delivery commands

`delivery` records what a coding agent did, reviews that evidence, and captures
the human decision.

| Command | Purpose |
|---|---|
| `delivery report [ref] --init` | Scaffold a conservative completion report at `.specgate/completion-<ref>.json` |
| `delivery peer-review [ref] --init` | Scaffold an independent agent review bound to the latest completion event and its exact Git receipt |
| `delivery peer-review [ref] --file <path>` | Record that review and rerun the delivery review |
| `delivery submit [ref]` | Run the whole tail: report → gates → review → status |
| `delivery review [ref]` | Trigger the delivery review alone, without reporting new completion evidence |
| `delivery status [ref]` | Show the authoritative delivery review verdict for a work item |
| `delivery approve [ref]` | Record a human approval of the delivery |
| `delivery reject [ref]` | Record a human rejection of the delivery |
| `delivery handoff export <ref>` | Write a committable review request for a reviewed work item |
| `delivery handoff show --file <path>` | Render a committed review request read-only, without a workspace or server |

Noninteractive callers must add `--yes` to the commands that record a decision.
Local `delivery approve` and `delivery reject` also require `--review-id` from
the status being reviewed. Use the exact
next command printed by status.

### Completion scaffold

`delivery report --init` writes a deliberately incomplete report: a required
blank `agent.name`, empty affected files, one `pending` check per unique
declared verification binding (or a `tests` fallback), and `not_done` criterion
claims. Fill `agent.name` with the coding agent's stable name before
submission.

`pending` is a scaffold placeholder. Submission without `--run-checks` rejects
it until the agent records `pass`, `fail`, or an explained `skipped`.
Submission with `--run-checks` accepts it only with a runnable command and
replaces it with the observed result. The scaffold captures Git identity but
defers delivery-scope comparison until `affected_files` is supplied at
submission.

`delivery submit` accepts only `coding_agent.completed`. Both the direct and
scaffolded paths reject missing completion-agent identity, pending checks that
will not be executed, `satisfied` claims without evidence, and passing checks
without runnable commands — all before any network call.

### Review and assurance

`delivery peer-review --init` binds the review to the latest completion event
and its exact Git receipt. The reviewer must cover every canonical criterion
exactly once, and the peer must differ from the completion agent. The CLI
preserves the receipt in the bound scaffold so the CLI verifies the same
completion. For a check-bound Local criterion, the peer review uses the check
result from that bound completion instead of asking the reviewer to duplicate
the completion's `checks[]`. An exact retry of the latest Local peer-review
returns that record while its completion binding remains current. If another
review intervened, resubmitting the earlier assessment records a new review;
historical records stay unchanged and status follows the latest assessment.
A mismatched Local completion or Git receipt returns conflict (exit `4`); refresh the scaffold
and review the latest completion before submitting again. `agent_attested` passes require a bound peer review or
human review.

A human may accept an advisory or false-negative review without hiding its
evidence verdict. The completion reporter cannot approve its own delivery, and
a new completion needs its own review and human decision.

Human `delivery status --detail` output separates evidence assessment,
assurance source, human decision, and recorded Git receipt. JSON exposes the
authoritative `verdict` and the optional `evidence_verdict`.

Cited local evidence paths must resolve to regular files inside the current
project; symlinked or outside-project files are rejected before they are read.
Each citation records a SHA-256 digest of the file plus a `grounding.status`
saying how the excerpt was anchored: `grounded` when an explicit `line` or a
`heading` present in the file located it, `heading_not_found` when the cited
heading is absent,
`line_out_of_range` when the cited line is past the end, `empty_at_anchor` when
the anchor resolves to a blank line, and `unanchored` when the citation names
only a path. Only `grounded` carries an excerpt — a path
that merely exists is not evidence for one criterion, so the other statuses are
reported rather than papered over with the head of the file. `submit --run-checks` previews and confirms the
completion file's commands, re-executes the non-skipped checks through `sh -c`,
marks those rows as observed by the SpecGate CLI, and submits the observed
statuses. Local status then labels that assurance `locally reproduced`. The
prior status stays in `checks[].claimed_status` when it changes. Running a
`pending` check records its first result. The assurance line reports
`corrected N agent-reported check result(s)` only when a CLI result contradicts
a reported `pass` or `fail`.

Use [Hand a review to a teammate through the repository](#hand-a-review-to-a-teammate-through-the-repository)
for the committable `delivery handoff` flow.

### Git receipts and freshness

Git receipts retain full checkout provenance while labeling changes outside
`affected_files` as unrelated checkout state. A receipt with an `origin` remote
has `freshness_scope=shared_repository`: `change status` compares repository,
branch, base, HEAD, and diff digest. A solo checkout without `origin` has
`freshness_scope=local_checkout`: it compares branch, HEAD, and working-tree
digest only in that checkout and never claims shared provenance. Configure an
origin when another machine must independently identify the repository.
When that local checkout is clean, SpecGate does not claim committed
`affected_files` are absent because no remote base exists to compute them.
Mismatches are explicit stale warnings, and unavailable non-Git metadata never
claims freshness.

A received Local store starts a fresh checkout generation. Local-checkout
receipts from a previous generation are historical. Shared-repository receipts
may still compare the repository, observed base, branch and HEAD. When only
those endpoints match, a dirty receiving checkout or a missing or different
working-tree digest leaves working-tree comparability unverified. Previously recorded tests
retain their source provenance. Pending reviews need the receiving
`basis_digest` from current CLI or browser status before a decision. Existing
decisions stay recorded, and inspecting status runs no tests.

`change status` also returns `criteria` — each acceptance criterion with its
verdict, the reason, and any `verification_binding` — so a human deciding
acceptance sees which criterion is weak and which check decided it without
reopening the completion file. `change submit` returns that same payload, so a
caller does not need a second `change status` call.

For a bound criterion the reason names who observed the deciding check, because
`--run-checks` is opt-in and the difference decides how much the verdict is
worth: `observed by the SpecGate CLI` when re-execution produced the status, and
`reported by the coding agent, not re-run` when nothing re-ran the command. A
`pass` of the second kind is the agent's own claim; treat it as a claim.

The reason also names a weak citation — a heading absent from the cited file, a
line past its end, or a path with nothing anchoring it — without changing the
verdict. The bound check is what the criterion was declared to depend on, so a
wrong pointer to the proof is reported as a citation defect rather than a broken
feature.

## Setup and identity

| Family | Purpose |
|---|---|
| `doctor` | Check the Local store, identity, workspace, and setup. The command reports recovery guidance when setup is incomplete. |
| `version` | Print the installed CLI version; equivalent to `specgate --version` |
| `user` | Create or select the local user with `user login` and clear it with `user logout`. `user list` enumerates local users and `user current` shows the selected one |
| `workspace` | Enumerate Local workspaces with `workspace list`, add one with `workspace create <name>`, and select or bind the workspace. |
| `plugins` | Install and verify Codex, Claude Code, and Cursor IDE plugin files. A project-local Claude install also writes `.claude/specgate-hooks/` and merges two entries into `.claude/settings.json`: `Bash(specgate:*)`, because the installed skills drive the CLI and an unpermitted first command stalls the work, and a `SessionStart` hook, which routes implementation work in a SpecGate-governed repository to a lifecycle phase without the user naming the tool. A Git repository is governed when its root contains `.specgate/`, which `specgate init` and `workspace bind` create; an unrelated ancestor such as `~/.specgate` does not govern it. An existing settings file is merged, never replaced, and keeps its file mode. The CLI reads its embedded plugin package. |
| `ui` | Start the embedded Local review UI in the foreground on loopback; see [Local review UI](#local-review-ui) |
| `open`, `update`, `uninstall` | Open the live Local review URL, refresh the CLI, or remove user-local setup. |

## Local review UI

`specgate ui [work-ref]` starts an embedded browser review surface for the
selected Local workspace and current repository. It listens on `127.0.0.1`;
`--port 0` (the default) asks the OS for an available port. Values outside
`0`–`65535` return usage exit `2`. A listen failure returns unavailable, exit
`5`. `--no-open` skips
launching the browser. `--json` emits a startup envelope with the actual URL;
the process remains in the foreground until interrupted.

While it runs, `specgate open <work-ref> --print --json` returns the current
session's URL for that work in the bound workspace. Bare `specgate open`,
`specgate open work`, and `specgate open reviews` all target the Local work
queue. `--print --json` prints the URL without opening a
browser. URLs require a live matching session, carry no session credentials,
and work on the machine serving the UI.

The UI shows real Local work, immutable approved context, criterion evidence
and the current completion's reported checks and summary. Its inline evidence
comparison uses the latest earlier human-decided review for the same work and
identifies both review and completion pairs. Unavailable comparison history
does not prevent inspection of the current delivery.
Human accept/request-changes actions require the exact displayed review,
completion and applicable acceptance basis. Requesting changes requires a
note containing at least one non-whitespace character. Concurrent changes return a
conflict and require a new inspection. The next actor is not inferred from
freshness or test results. Original content remains untranslated when changing
the EN/VI interface.

Maintenance actions preview the exact updater scope/version or eligible owned
cache entry, then revalidate before mutation. Cache cleanup preserves Local
history, work/spec/evidence/acceptance data, backups and unrelated files.
The Windows preview reports that cache cleanup is unavailable.
See [Local review](../guides/local-review.md) for the human workflow.
The optional [notification pilot](../guides/local-notification-pilot.md) is a
foreground script in the source checkout. It requires Node.js 26 or later and
works with the Local CLI while the browser UI is closed.

The selected local user and workspace are stored in CLI config. Use
`specgate user login --workspace <name> --display-name <name> --username <name>`
to create or reuse a Local user/workspace pair. `specgate user logout`
clears the selected user and workspace bindings while preserving stored Local
users, workspaces, and history. Identity names are cooperative attribution.

`specgate init` automatically binds the Git checkout where initialization
runs. For another project, run `specgate workspace bind` inside its checkout to
bind that checkout to the currently selected workspace. Use
`specgate workspace bind <slug>` to bind a named workspace directly. The CLI
records that workspace in the user config under the checkout's Git root path.
Binding a named workspace does not change the global workspace selection; use `workspace select` when the global default should change.
Future commands run from that checkout or its subdirectories use the project
workspace instead of the global workspace.

In Local mode, `workspace select <slug>` requires an explicit workspace slug
(missing or blank input returns usage exit `2`). It updates the global
selection without prompting or removing project bindings; an existing project
binding therefore continues to win inside that checkout. Use `workspace bind
<slug>` to change that project's binding explicitly.

Local quick-work body validation and feature-backed `work create` reject blank
acceptance criteria and ambiguous check bindings with validation exit `2`,
before opening the store. Quick-work body validation also requires a title.
Supply each criterion explicitly with a repeated `--ac` flag; feature-backed
work additionally checks each `@source:<id>` against its pinned artifact.

Workspace-scoped commands refuse to run from a Git checkout that has only a
global workspace fallback. The error shows `specgate workspace bind`; the command stops before work-state access. An explicit `--workspace`,
`SPECGATE_WORKSPACE`, project binding, or repo `.specgate/config` default
satisfies the boundary. Outside Git checkouts, the global workspace remains
available for administrative commands.

Use `specgate workspace current` to see the active workspace and whether it
came from an override, project binding, repo default, or global selection. When
the current checkout is using the global workspace, `workspace current` shows
the Git root as unbound and prints the matching `workspace bind` command.
Use `specgate workspace unbind` inside a Git checkout to remove
only that project's workspace binding. This config-only operation keeps the
selected user, global workspace, Local database, and other project bindings;
it does not contact a server. Effective workspace precedence is:

1. `--workspace <slug-or-id>`;
2. `SPECGATE_WORKSPACE`;
3. current Git project binding;
4. repo `.specgate/config` workspace default;
5. global workspace;
6. no workspace.

## Local cleanup

`specgate cleanup --work` selects transient entries under the current project's
`.specgate/work` and generated completion and peer-review scaffolds. Use
`specgate cleanup --work --dry-run` to preview. Noninteractive deletion requires
explicit `--item` values and `--yes`. Approved specs, active data, and unrelated
files remain in place.

## Local CLI uninstall

In Local CLI mode, `specgate uninstall` removes CLI configuration and globally
installed managed plugin files while keeping the configured SQLite store.
Project-local plugin files in repositories are preserved and remain under
normal repository file ownership. Purging requires a default-No terminal
confirmation naming the store, or explicit `--purge-data --yes` in automation.
Redirected input without `--yes` is refused before removing files.
Purge refuses conflicting nonempty environment, current-project and global
store selections, even with `--yes`; resolve that conflict before retrying.
The target must already be stored in project or global configuration;
`SPECGATE_LOCAL_DIR` alone never authorizes deletion. For an unambiguous
configured store, it deletes only SpecGate's `state.db`
and SQLite journal files in that store;
other files in the selected real directory are preserved. Symlinked state
directories and database files are refused, non-regular database paths are
refused. An emptied store directory and `.specgate/` parent may be removed only
after managed SQLite files were purged; directories with no managed files or
with other files are preserved. It does not invoke Docker. The interactive prompt
offers only IDE plugin files and Local SQLite state.

Unknown files added inside a managed plugin directory survive uninstall, but
the directory's SpecGate ownership marker is removed. Reinstall refuses such
an unowned leftover rather than overwriting it. Inspect the reported
`preserved_paths`, move the retained directory to a user-owned location if you
want to keep its notes, then retry installation. Do not delete those files or
restore ownership markers merely to bypass the refusal.

## Interaction

When a work reference is optional, the CLI may show work needing attention and
ask you to choose. Artifact approval, artifact promotion, and delivery approval
or rejection require explicit `--yes` in every output mode.

Automation should pass all required values and use `--json --no-input`.

## Token-conscious output

`specgate work context` returns the full Context Pack because coding agents need
the complete implementation contract. Artifact-backed work includes approved
artifact context. Quick work uses the persisted title, intent, and acceptance
criteria. Other potentially large commands default to references or summaries:

- Read a work's pinned document with
  `specgate work context <ref> --document <path> --role <role> --json`.
- `specgate gates check <artifact-id> --json --summary` runs readiness but omits
  evidence bodies and timestamps. It keeps the aggregate, gate
  states, hints, and dispatched IDE-agent task IDs. Use
  `specgate gates results <artifact-id> --json` to read the stored detailed
  evidence without running readiness again.
- `specgate artifact publish --file artifact.json --preview` expands local
  document sources and shows exact paths, roles, target, base version, and
  non-goals plus the governance policy, required roles, evidence, gates, and
  frozen rubric sources. Preview publishes nothing and persists no preview state. After human confirmation,
  run the same command without `--preview`. Add `--compare <artifact-id>` only when a
  read-only comparison with one explicit published version is needed. That
  lookup is constrained to the package's explicit workspace or the selected
  workspace; it fails locally when neither is available. It reads
  artifact metadata and file hashes, never prior content, and reports added,
  removed, changed, and unchanged path-role mappings; it still performs no
  publication. `--compare` requires `--preview`. The publish command accepts
  Git-root-relative `repo_file`, manifest-relative `source_file`, or absolute
  local `file_url` entries in `documents[]`; every document must set exactly one
  of `content`, `repo_file`, `source_file`, or `file_url`. The CLI reads those files as raw UTF-8 content. Unknown package or document fields are rejected instead of being
  silently ignored. A `repo_file` must stay inside the nearest Git root and a
  `source_file` must stay inside the package directory after symlink
  resolution. Sources must be regular UTF-8 files, cannot themselves be
  symlinks, cannot change between validation and opening, and are limited to
  1 MiB. Preview output shows the resolved absolute `source_path`. Publish files use `request_type` for the
  work kind; omitted values default conservatively to the structured `unknown`
  value, without inferring intent from document prose. The store assigns `version`; use
  `base_version` only when publishing an update from an existing version. Publication requires it to match the latest stored version.
  A stale or mismatched Local base is `conflict` (exit 4), not an unavailable
  service: inspect the latest artifact and revise the package before retrying.
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
connection. Its store path is the active state directory after environment,
project-binding, and global-config precedence, paired with that store's ID;
it is not necessarily the global default. It then reports repository binding,
`sh`, and optional IDE-plugin files separately. A stale repository binding is
actionable; absent optional
plugin files do not make CLI-only Local mode unhealthy. After `user logout`,
an initialized store remains inspectable: `doctor` returns a successful
diagnostic envelope with identity and workspace marked `missing` and a
`specgate user login` recovery command. Plain output says setup is incomplete,
not ready. The diagnostic does not restore a selection, change configuration,
or delete stored users/workspaces. Failure to open or read the store still
returns an error. Plugin files do not
prove a running IDE loaded them. Restart the IDE and verify from a new session.

A configuration that selects the retired remote setup returns incompatibility
exit `6`. Preserve that configuration and use a prior compatible CLI if you
still need to operate the deployment. Follow [independent Local setup](../guides/operate-specgate.md#previous-installations) for the separate configuration, legacy
environment override, and repository binding steps.
SpecGate has one supported setup mode: Local. The old `--mode local` spelling
still works for compatibility, but new commands can omit it.

```bash
specgate update
```

The updater installs the latest stable GitHub release, falling back to the
newest prerelease only when no stable release exists. An explicit `--version`
may select either. It refreshes only already-installed global Codex, Claude
Code, and Cursor plugins carrying SpecGate CLI ownership markers. Other plugin
installations stay untouched. Refresh a project-local install with the matching
`specgate plugins install --project-local` command.

On POSIX systems, the updater derives the install directory from the current
executable, resolving symlinks when possible. The directory is passed to the
installer as one argument, including paths with spaces. A directory at the
binary destination is refused. In `--json` and `--json-progress` mode,
installer progress goes to stderr so stdout contains only JSON output.

On native Windows, `specgate update` downloads the matching release ZIP and
published checksum, verifies SHA-256, and replaces the current executable
without requiring `sh`.

Released CLI builds check GitHub releases in human/plain output and warn when a newer public release
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

`plugins install` and `plugins doctor` use the package embedded in the CLI,
including before initialization.
Before writing, install validates at most 16 safe skill names and preloads no
more than 32 MiB of required package files. Project-local Claude installation
also validates `.claude/settings.json` before any plugin file is written.
Malformed JSON returns a `validation_failed` envelope; a new settings file uses
owner-only permissions, while an existing file keeps its current mode.
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
the current `PATH`. For project-local Claude Code, it also validates the
CLI-owned hook files plus the `Bash(specgate:*)` permission and SessionStart
registration in `.claude/settings.json`.

Interactive `plugins install` and `plugins doctor` show a checkbox list for
Cursor, Codex, and Claude Code when `--agent` is omitted. The default selection
prefers locally detected IDE tools and falls back to all agents when none are
detected. Interactive `plugins install` also asks whether to write global user
files or project-local files. Scripts can still pass `--agent all`,
`--agent codex`, or a comma-separated subset.

Local automation can install an embedded IDE target with
`specgate plugins install --agent codex --project-local --no-input`.
Use `--project-local` when the current repository should vendor its IDE files.

## Related guides

- [Use the SpecGate CLI](../guides/cli-workflow.md)
- [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md)
- [Operate SpecGate](../guides/operate-specgate.md)
