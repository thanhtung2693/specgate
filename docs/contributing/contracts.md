# Contracts

This reference describes the supported Local CLI, embedded review API, and
IDE handoff. The [CLI reference](../using-specgate/reference/cli.md) owns
public command syntax. Prior Full service contracts remain in historical
revisions and [ADRs](adr/README.md).

## Local verification pin and pickup projections

Local may store one optional immutable verification contract per work. The
contract digest binds workspace/work identity, the existing Context Pack digest,
fixed `sh` shell, normalized repository-relative check directories, exact check
commands/names and bindings derived from stored acceptance criteria. Actor and
timestamp are recorded separately from digest identity. Pinning is permitted
only before the first report and is rechecked transactionally; contracts cannot
be amended. Existing work remains `unconfigured` unless explicitly pinned.

Local submission resolves workspace/work and validates the Context Pack before
executing commands. Pinned reports carry `verification_contract_digest` and the
exact configured checks; mismatches are rejected before execution and again
inside the report transaction. Pin status is separate from check outcome and
human acceptance. Local same-user storage is not an authentication or hostile
agent security boundary. Use a complete Local backup to keep these records.

Local report-enabled checks use exact JUnit identities, not text matching.
Every reported failure/error fails the observation, including unselected cases;
selected cases must each occur once and pass. Shared identities across criteria
are deduplicated only for execution, not removed from criterion bindings.
Optional testcase `status` attributes support `run` and `notrun`. Optional
`result` attributes support `completed`, `suppressed`, and `skipped`. The
`notrun`, `suppressed`, and `skipped` values count as skipped even without a
`<skipped>` child. A `completed` result can still contain `<failure>` or
`<error>`. Unknown, duplicate, or contradictory outcome information invalidates
the report. Other metadata attributes do not affect the outcome.
When a selected case is missing or the report is invalid, the Local criterion
reason includes the CLI-observed report diagnostic; agent-supplied check details
are not promoted to that trusted reason.
The CLI supplies the output path to the actual shell process, verifies Git
ignores it, and retains normalized evidence rather than XML or output tails.
Run receipts bind work/context/pin/check/completion identity and endpoint
observations. Changes to Git or watched-file states/digests during the run fail
the check, including Git-ignored watched paths. Unavailable observations stay
explicit. Equal watched endpoints that already differ from the pin retain a
separate drift warning.
Cleanup must not remove unrelated files in the run directory.

Watched files resolve inside the checkout and use confined, bounded reads
(16 MiB each), rejecting oversized or unstable observations. Current watched
digests participate in enhanced acceptance material: changing them after status
invalidates the old basis token. This is a review freshness boundary, not a
claim that an input was intentionally weakened or that a same-user runner is
trustworthy.

Local `work resume` and context summary/document views are additive read-only
projections. They never select work from branches, revise approved content,
change Context Pack digests, or summarize away acceptance criteria. A document
index is not a substitute for reading the indexed scope and non-goals. Resume
path deltas compare recorded file bytes/modes, not receipt timestamps; plain
and JSON views retain counts and explicitly label limits.
Feature-backed Local work reads retain the workspace-scoped `feature_key` on
list, show, summary, and resume, matching creation. Quick work has no feature
key. Resolving this display identity does not change the pinned artifact or
Context Pack digest.

Local artifact impact compares one exact declared lineage pair. Its source
transitions contain recorded before/after fields and literal change flags, not
semantic equivalence. Old work remains bound to its original artifact. Overlap
hints use exact linked source IDs or latest reported affected-file paths; path
comparison requires a shared repository receipt or same-checkout ID. Missing
lists/provenance stay unknown or noncomparable, and an empty hint set is not a
parallel-safety claim. A base inventory with no criteria and no prior lineage is
unknown; a target cannot turn that legacy absence into a declared empty base.
Selected checkpoint-to-current deltas are part of the acceptance-basis digest;
unavailable comparisons remain visible as unknown rather than unchanged.
Local acceptance inspection reads displayed work, completion, review, peer,
coverage, historical decision and the current basis from one SQLite snapshot.
Concurrent writes must not produce a displayed old review with a newer basis
token. Checkout freshness remains a separate endpoint observation, not an atomic
filesystem/database snapshot.
Undecided delivery retries retain the latest applicable human rework note from
the same work and workspace. Status returns the note as `guidance` and its
origin as `guidance_review_id`, separate from the current `review_id`. A newer
human decision supersedes that guidance, and acceptance clears it from status.
The basis retains normalized current-checkout freshness and peer-review state
alongside their identities and gaps, so historical decisions remain inspectable
without decoding opaque hashes. Matching endpoints do not prove continuous
immutability or correctness.
Compact Local risk counts classify unavailable or noncomparable current
freshness as unknown using that normalized basis state, not its rendered prose.
Historical passing test results stay passing; unknown provenance is separate.
Criterion gap categories use their structured check, selected-case, grounding,
and freshness outcomes. User-chosen names and paths are opaque data and never
classify a gap by containing words such as `fail`, `changed`, or `unavailable`.
For legacy records without a normalized basis state, the CLI uses the actual
checkout-comparison outcome; an unchecked comparison is an unknown risk.

An explicitly selected acceptance checkpoint must belong to the same work,
workspace, and comparable checkout. An unavailable or noncomparable comparison
is invalid for basis generation and decisions; resume may still display that
limitation without claiming an unchanged delta.

A received Local store has a fresh checkout generation independent of its
store, workspace, work, and artifact IDs. Normal and enhanced v1 stores omit
`checkout_generation`. Received stores use v2 and include it in new
checkout observations, including checkpoints, Git receipts and verification
run endpoints. Default resume selects a checkpoint for the current checkout
identity. An explicit `--since` may display a historical checkpoint and its
note while reporting a generation mismatch as `noncomparable`. A historical
checkpoint is ineligible for acceptance.

CLI and browser inspection compute a receiving-generation basis for a pending
review, including reviews that previously needed only a review ID. A human
decision requires that new basis. Historical verdicts, checks, review IDs
and recorded acceptance bases retain their original values. Inspection runs
no checks and leaves the lifecycle state and next actor intact. When the
completion comes from a different generation, `historical_verification`
labels the stored results in status and the current acceptance basis.

The first enhanced Local write snapshots the legacy store inside a private
temporary directory before publishing a mode-`0600` backup without overwrite.
The compatibility marker, guards, and first record then commit together. The
backup is an older recovery point, not a lossless downgrade.
Delivery-handoff export rejects this recovery backup as a
destination, including symlink and hard-link aliases, regardless of which
workspace is selected in the store.

## Local snapshot transfer

The CLI owns `transfer prepare`, `transfer inspect`, `transfer receive` and
`transfer activate`. The v1 archive carries a private SQLite snapshot of
the entire selected Local store, including committed WAL data. The
versioned manifest declares inventory and a pickup workspace, work ID/key
and Context Pack digest, with an optional checkpoint and note. The
workspace/work/checkpoint is a bookmark. The manifest and picked records
are validated against the staged SQLite copy used for the exact published
archive. The payload SHA-256 checks the DB and the archive SHA-256 binds
the manifest to it.

Inspect and receive are independent of active Local config. Receive only
publishes a complete new private directory and receipt. Receive and activate
reject surrounding whitespace in the store path before path normalization.
The database is validated read-only before supported migrations or generation
writes in staging. The receiving store keeps the original StoreID and domain IDs and
gets a fresh compatibility-protected generation every time. Activation
validates the receipt against that generation and pickup relation. Subsequent
legitimate history writes do not invalidate the original receipt's inventory.

Activation is an explicit binding change for one repository. It previews the
effective old and new path, workspace and generation, checks the selected
config and environment, confirms, rechecks and saves atomically. A failure
after the save reports a committed unverified binding and preserves both
stores. The pickup command is ordinary `work resume` with explicit
`--since` for a selected checkpoint. Actor ownership and decisions remain
governed by the ordinary Local status. `delivery handoff/v1` retains its
bounded review request contract.

## HTTP Surfaces

### Optional Local review surface

`specgate ui` serves embedded assets and an allowlisted JSON API on loopback
for one fixed Local workspace/repository. `open --print --json` returns a clean review URL
only for a live matching Local session; URLs contain no session credentials.
The loopback listener trusts all local accounts and processes that can reach it.
Session cookies, exact Origin and CSRF checks protect browser interactions
against other origins. Local HTTP has no OS-user authentication. The private
discovery descriptor locates the matching session and confers no HTTP access.

The Local UI uses the CLI's existing `change status` vocabulary and immutable
context readers. `next_actor`, human decision eligibility, evidence provenance
and freshness are independent fields. UI translation never changes original
scope, criteria, claims or notes. No fixture data is substituted for read errors.

`GET /api/work` includes the current completion's submitted checks, in report
order, with recorded command, result, source, detail and selected outcomes
when available. It exposes `review_comparison` for a displayed delivery. The
comparison names both the displayed review/completion and the latest earlier
human-decided review/completion in the same workspace and work. It uses stored
criterion, check and summary material. Run IDs, timestamps and report digest
churn alone do not mark evidence changed. The prior decision and note remain
inspectable. `no_baseline` and `unavailable` describe the comparison only.
Absent optional fields in earlier reports remain readable. A malformed field
or unusable row makes the earlier evidence unavailable while preserving its
review identity and decision note and keeping the current evidence accessible.
The projection does not read historical checkout content or change the
decision token, source endpoint or freshness calculation.

The notification count and the human work queue use the authoritative
`human_reviewer` and `maintainer` next actors from the work list. A failed read
shows that the list is unavailable. Settings indicates available CLI updates
independently of the work count. Filtered queue results show the displayed
count against the workspace total. Approved context uses each document's
path and role to distinguish multiple snapshots with the same path.
The **Scope** tab reads those approved snapshots. Criterion rows distinguish
the agent claim, CLI-observed command and attached selected tests. An optional
test without a binding is separate from missing required results. Queue layers
indicate delivery presence, without inferring a delivery count.

`GET /api/works` also includes each item's `review_id` and `completion_id`,
both empty before its first delivery. The UI compares those identities with
the currently inspected work during the existing 30-second poll. A newer
delivery adds a notice without replacing evidence, a draft note, an open
source view or a dialog. **View latest** reads fresh work details and decision
identities on the same tab and retains the draft note for that work. Queue read
errors still show that the list is unavailable. A confirmation opened after a
new delivery is known directs the reviewer to **View latest**. An already open
confirmation keeps its original identity for server-side revalidation.

Decision writes name the exact reviewed work, review, completion and basis.
The Local review API requires a note containing at least one non-whitespace
character for requested changes. The Local owner revalidates those
identities before recording a human decision. The browser cannot approve a
newer completion by reusing an old confirmation. Historical acceptance remains
separate from current checkout freshness. An in-flight decision response can
update the work list, but only updates the detail view and confirmation that
initiated it while both are still current. Update and cache writes use explicit
previews; neither accepts arbitrary commands, installation URLs or file paths.
Cache cleanup requires support for verifying file ownership and link count.
On Windows, the preview reports cleanup is unavailable and confirmation is
refused.

`GET /api/source?ref=<work>&criterion=<canonical-id>` reads only the selected
workspace's latest completion citation for that canonical acceptance criterion.
It rejects additional path/revision selectors. The response includes
`completion_id` from that exact completion, `path`, `citation`,
`origin: current_checkout`, a captured `head_revision`, `source` and `diff`.
The browser must compare `completion_id` with its displayed completion before
mounting the source view. This identifies citation ownership; it does not pin
current checkout bytes or promote them to observed test evidence.

`source` contains `available`, `start_line`, `highlight_start`, `highlight_end`,
`lines: [{number, text}]`, `truncated`, and an unavailable `reason` when needed.
Only `evidence.kind` values `file`, `test`, and `code` with a numeric positive
integer `line` and optional `end_line` are readable. Early citation rejections
leave `head_revision` empty. `head_unavailable` means Git HEAD could not be
resolved after inspection. The viewer includes about five context lines and caps
output at 120 lines and 64 KiB of excerpt text. Files must be regular UTF-8 text
without NUL, at most 1 MiB, confined through `os.OpenRoot` to the bound repository.
Traversal, symlinks, Git internals, credential/private-key paths and Local
storage are excluded; `.specgate/work` remains readable. File identity,
size and modification time must remain stable while reading.

`diff` contains `available`, `base: HEAD`, `text`, `truncated`, and an unavailable
`reason` when needed. The diff compares the confined checkout bytes with the
raw blob at the exact file path in captured HEAD. It requires an exact regular
file in the index for a present checkout file. A deleted file can use the regular
HEAD blob even after staging the deletion. An absent HEAD path is valid for an
exact index addition. Directories, symlinks and Git submodule entries cannot
serve as diff inputs. The same UTF-8, sensitivity and 1 MiB bounds apply to the
HEAD blob, whose size is checked before reading it. Changed files with more than
32,768 lines combined have no diff. The current excerpt remains available when
it meets its own limits. Diff output is capped at 64 KiB. Git lookups use literal paths and
a timeout without shell, external diff, clean filters, textconv or filesystem
monitor helpers. Untracked files report `untracked`. A citation that moved out of
range can still show its textual diff. These read-only observations may change
while the UI is open and never alter completion claims or acceptance pins.

## Identity and Workspace

Local users identify an operator for workflow decisions and local history.
Workspaces scope artifact, work, delivery and queue reads. The CLI honors
an explicit workspace override, Git project binding, repo default, then global
selection. Workspace-scoped operations in an unbound Git checkout require
an explicit override or repo binding/default. The optional loopback UI fixes
its active store, workspace and checkout at startup. See the
[configuration precedence](../using-specgate/reference/configuration.md#cli).

## Work and Artifacts

Quick work has no lead artifact. Artifact-backed work pins the exact approved
artifact version. Quick work accepts a title and explicit criteria. For an
artifact-backed slice, the agent reads every explicit spec-role source and
submits each human-confirmed criterion with a repeated `--ac`. Stored criteria
drive completion and peer review.

A handoff names the work title and stable ID and provides the authoritative
`next_command`. Optional browser handoff uses `specgate open <work-ref> --print --json`
only when the human chooses browser review. An agent starts the UI only on
request. Use the exact URL the CLI returned.

The product hierarchy is:

```text
Workspace
  -> Feature
     -> ChangeRequest / work item
        -> lead artifact
        -> Context Pack
        -> delivery evidence
```

| Role | Meaning |
| --- | --- |
| `spec` | governed source of truth: intent, scope, acceptance criteria |
| `design` | architecture, data model, implementation shape |
| `plan` | task breakdown |
| `verification` | test plan and evidence |
| `research` | background and decisions |
| `reference` | supporting non-normative material |
| `custom:*` | caller-specific role |

Product briefs, requirements, and PRDs normally use `spec`; callers needing a
separate label may use `custom:prd`.

Selected local packages are framework-agnostic at this boundary. Callers map
explicit files into the same `{path, role, bytes, provenance}` package; no
framework detection affects storage. A published version stores exact
normalized repository-relative paths, CLI-computed file SHA-256 values, a
deterministic manifest digest, and versioned source provenance. Publication stores the package in the selected Local workspace.

The CLI source locator is explicit and never changes the artifact `path`:
`repo_file` resolves from the nearest Git root, `source_file` resolves inside
the manifest directory, `file_url` names an explicitly selected external local
file, and `content` is inline. Exactly one is accepted. The CLI replaces local
locators with UTF-8 content for the Local store and rejects
filesystem escapes or unsafe file types.

## Governance Policy

Policy snapshots use `specgate.policy/v1`. Each records the required roles,
topics and checks, approval and evidence policy, gate versions, and each
frozen rubric and digest. Role comes from the explicit document mapping.

Local artifact snapshots use the fixed `local-standard` contract: Standard
governance with `plan` and `spec` roles, the Standard topics and
gates, `human_required`, and `attested_ok`. Local embeds default rubric content
and does not resolve Light or Enhanced.

The CLI resolves optional `.specgate/rubrics/` files at the explicit Git-project
boundary before Local preview or publication. Only `scope_clear.md`,
`spec_completeness.md`, `acceptance_criteria_verifiable.md` and
`spec_repo_drift.md` append instructions to existing embedded gate definitions.
The Store receives resolved text, never a CWD or filesystem resolver. Missing
files preserve byte-identical default policy JSON/digests; optional gate
`source` fields are omitted for defaults. Custom source is a project-relative
path, and its preview digest hashes the full effective text. Publication freezes
that text/source in the existing policy snapshot; gate dispatch and result
validation use only immutable snapshot/task records. Later edits, removal or
new versions never rebind historical work or tasks.

Project reads use a confined directory handle, reject symlink directories/files,
nonregular inputs, unknown `.md` filenames, empty/invalid UTF-8/binary-control
text, and content over 64 KiB per active file. Reads are bounded and detect
ordinary replacement/growth during observation. Preview performs no writes;
rubrics remain user-owned source, outside transient work cleanup. Text cannot alter gate identity, structural checks, approval or
evidence policy, or human acceptance authority.

## Context Pack

A Context Pack is the implementation handoff contract for a coding agent. It is
derived from the work item, acceptance criteria, approved/canonical artifact,
policy snapshot, gate state, and applicable skills.

It includes:

- execution brief;
- approved spec/design/plan/verification material;
- acceptance criteria;
- scope and blast radius;
- unresolved gate feedback;
- applicable skills;
- outstanding delivery-review feedback when rework is required.

Coding agents read it through the CLI:

```bash
specgate work context <work-ref>
```

## Coding-Agent Workflow

The normal coding-agent workflow publishes through `specgate artifact publish`
and consumes work through the CLI plus IDE skills installed by:

```bash
specgate plugins install
specgate plugins doctor
```

IDE plugins provide behavior: read the Context Pack, stay inside scope, report
blockers, submit delivery evidence, and read delivery review. They do not add
product scope or bypass approval.

Claude Code and native Codex plugins register a synchronous `SessionStart`
reminder without a source matcher, including resumed sessions. Both return
`hookSpecificOutput` with `hookEventName: "SessionStart"` and
`additionalContext`. Claude project-local refresh replaces only SpecGate's
command registration and preserves other hooks and their matchers. Cursor uses
an `alwaysApply` rule; project-local Codex installs contain skills only.

## Delivery Evidence

Local delivery decisions require the exact `review_id` displayed to the human.
`change accept|request-changes` and `delivery approve|reject` require
`--review-id` and `--yes`. The store compares that ID to the latest review in
the decision transaction; a replacement report returns conflict without making
a decision. Existing reports need no migration: read status to obtain their ID.

Local completion verdicts reject duplicate or unknown criterion IDs regardless
of claim. Every canonical criterion must have one evidence-backed satisfied
claim for a passing review; contradictory entries never cancel each other out.

Delivery reports are append-only evidence. A completion report may include:

- summary;
- affected files;
- checks;
- per-criterion evidence;
- agent metadata and run id.

`coding_agent.completed` requires a non-empty `agent.name`. This identity is
what allows a later peer review to prove that a different agent inspected the
same completion receipt.

On CLI submission, a noncanonical draft is marked with the CLI-owned
`custom_completion_file` field; the machine-local path stays out of the
completion record. The submit response can use the path supplied to that
invocation. Later status asks for the revised report path explicitly when
this marker is present. Canonical and older unmarked completions retain the
default-file retry.

Canonical check names are free-form strings supplied by the reporter, but common
CLI scaffolds use `tests`, `lint`, `types`, and `build`. Each runnable check's
shell `command` is retained in the append-only receipt so an independent
reviewer can reproduce it. In Local mode, `delivery submit --run-checks`
overwrites each executed row with the observed status and
`source: "specgate_cli"`; status reports `locally reproduced` only when that
marker is present. CLI submission discards input `checks[].source` and
`claimed_status` before execution, including skipped checks; only this invocation
can stamp observation metadata.
Check names and statuses are trimmed consistently for confirmation, execution,
verdicts and evidence projections. A padded `skipped` status never executes;
normalizing a read does not rewrite historical report rows.
Local change status and resume project `completion_summary`, when present,
from the same completion as `completion_id`. `change submit` shows the summary
of its displayed review. JSON preserves the string verbatim. Plain output
frames agent-reported lines and neutralizes terminal control characters.
Criteria, check provenance, lifecycle state and decision commands use their own
recorded inputs.

The `deterministic` trust tier identifies a `bound deterministic check` that
supplies a required outcome for the criterion. Readback names the observer in
the bound criterion's reason as `observed by the SpecGate CLI` or
`reported by the coding agent, not re-run`.

Acceptance criteria may include a human-authored `verification_binding`, created
with:

```bash
specgate work create-quick "Title" --ac "Criterion @check:tests"
specgate work create --feature feature-key --title "Title" --ac "Criterion @check:tests"
```

The binding names a later delivery check. Removing the token for the criterion
text preserves whitespace within the rest of the text, including literal test
inputs. A bound criterion is resolved deterministically:

| Check status | Criterion result |
| --- | --- |
| `pass` | `met` with a satisfied claim. Other claims retain their verdict, including `unmet` for `partial` and `not_done`. |
| `fail` | unmet |
| missing or `skipped` | unclear |

Agents must not invent authoritative bindings. Model-emitted bindings are
ignored for deterministic trust.

The stored acceptance criterion defines the binding. Local review fails when
a bound check is missing or skipped and identifies the criterion and check.
Local peer review applies the same resolution.

A human decision names the latest reviewed completion and its exact review ID.
Enhanced work also carries the observed acceptance basis. The Local store
rechecks the selection transactionally. A new completion requires a new review
and a new human decision. Accepted work cannot receive further completion
reports. Keep `evidence_verdict` separate from the final `verdict`.

`policy_unavailable` indicates that delivery review could not interpret the
artifact's frozen policy. `delivery_review_outdated` means the latest completion
does not have a matching review. Both block decision against stale evidence.

### Delivery handoff bundles

`specgate delivery handoff export` writes a Local review request as a single
committable file carrying `schema_version`, `exported_at`, `source_mode`, the
work item, its delivery review, the bound completion report, peer-review status,
and a `checksum` computed over the bundle with the checksum field cleared. The
current version is `specgate.delivery-handoff/v1`; a reader that does not
recognize the value refuses the file rather than guessing.

Export drops every `criteria[].evidence.grounding.excerpt`, including excerpts
from older reports whose original source boundary is unknown. The retained
`status` and `digest` record how it was anchored. The bundle reader inspects
that record without re-verifying the original source. Export refuses the
active Local SQLite file as a destination.

`criteria[].evidence.grounding` is computed by the CLI. New completion and
peer-review input discards caller-supplied grounding before validation. Only a
current read of a regular file inside the working tree can stamp it; absent
paths and `--skip-evidence-check` cannot retain copied observations. Historical
stored reports remain unchanged. Its `status` uses:

| `status` | Meaning |
|---|---|
| `grounded` | An explicit `line`, or a `heading` found in the cited file, located the excerpt |
| `heading_not_found` | The cited `heading` does not appear in the file |
| `line_out_of_range` | The cited `line` is past the end of the file |
| `unanchored` | The citation names a path and nothing that pins it to this criterion |
| `empty_at_anchor` | The anchor resolved, but the line there is blank |

Only `grounded` carries an `excerpt`, and it always carries a non-empty one —
the agents' delivery review requires both together, so a blank anchor reports
`empty_at_anchor` rather than an empty grounded excerpt. Every status carries the
file `digest`. A
non-`grounded` status is reviewer-visible information, not a submission error —
the citation is recorded as weak rather than silently presented as verified.

The bundle is an unsigned, bounded review request. `handoff show`
re-derives the verdict from the report and acceptance criteria it carries and
reports any disagreement with the recorded verdict, so a bundle written under
weaker enforcement cannot present a stale pass as current. Reviewer decisions do
not travel in the bundle: human decisions stay with the owning store through
`change accept` / `change request-changes`. The reader directs the
author to current status in that store for the exact next action. Any
verification observations displayed by the reader come from the source report.

## Gate Runs and Trust

`gate_runs` is the shared evaluation history for artifacts and work items.

Gate states:

- `pass`
- `warn`
- `fail`
- `needs_human_review`
- `not_applicable`
- `pending`
- `not_run`

Trust is derived from the executor:

| Executor | Trust tier |
| --- | --- |
| human | human decision |
| deterministic | deterministic |
| IDE agent | agent attested |

Completion `git_receipt` payloads may set `freshness_scope=shared_repository`
when an origin URL identifies the checkout, or `local_checkout` when no origin
exists. Local scope supports CLI staleness comparison of branch, HEAD, and
working-tree digest in that checkout only. CLI freshness requires a matching
recorded `checkout_id`; a missing or different identity is noncomparable, not a
matching or stale observation. Missing scope remains compatible with old origin-backed receipts.

Across Local store generations, local-checkout receipts are noncomparable.
Shared-repository receipts can compare repository, observed base, branch and
HEAD. A dirty receiving checkout, an unavailable working-tree digest or a
different digest prevents a full freshness match even when the committed
endpoints match. Receipt digests hash a symlink's link text and require the
changed paths to be observed. A full endpoint match remains a Git observation.
Source verification retains its original provenance and outcome.

## Naming Rules

- JSON fields and enum values use lowercase snake case.
- User-facing feature keys use lowercase kebab case.
- Approved artifact snapshots and Context Packs govern implementation.
