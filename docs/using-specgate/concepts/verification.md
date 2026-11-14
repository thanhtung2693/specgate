# How verification works

SpecGate's promise is that a verdict means something: when a gate says `pass`
or a delivery review says `met`, a team should be able to say exactly what was
checked, against what context, and with what confidence. This page explains
how the checkers work and where their limits are. It describes what the code
does in the Local CLI.

If you want to act on a specific verdict, see
[Respond to gate failures](../guides/respond-to-gate-failures.md). If you want
to look up one gate, see the [gate catalog](../reference/gates.md).

## Two checkpoints, two different questions

Verification happens at two points in the delivery loop, and they answer
different questions:

- **Readiness gates** run before handoff and ask: *is this spec package good
  enough to build against?* They judge documents.
- **Delivery review** runs after implementation and asks: *does the evidence
  show the build satisfied the contract?* It judges the coding agent's
  account of the work.

Neither one approves anything. Both produce evidence for the human decision.

Delivery status keeps four facts separate:

1. **Evidence** — whether the submitted material is ready for human review,
   has gaps, needs human interpretation, or could not be evaluated because its
   governing policy was unavailable.
2. **Assurance** — where confidence comes from, such as an agent report, a
   locally grounded citation, a reproduced check, or peer review.
3. **Decision** — whether a human has accepted or rejected the delivery.
4. **Receipt** — which Git revision was recorded with the report. A recorded
   receipt is a comparison target, not proof that it is still the current
   checkout.

## What a readiness gate sees

The CLI dispatches frozen semantic tasks to the IDE agent. Each task carries
the artifact and policy digests, gate definition and rubric. The agent reads
the relevant explicitly mapped document roles and submits a structured result
with evidence. Store provenance identifies the result as `agent_attested`.
A successful dispatch may remain `not_run` while required tasks are pending.
Deterministic checks enforce required roles and the artifact snapshot pin.

## What the delivery reviewer sees

Local delivery review compares the exact stored acceptance criteria with the
completion report. Each canonical criterion needs exactly one supported
`satisfied` claim. Bound criteria use the named check result. Missing,
duplicate, unknown, skipped, and failed evidence remains visible in review.
The review covers submitted evidence. The human may also inspect the Git diff
and current source before recording a decision.

Before work exists, the coding agent reads every explicitly mapped spec-role
document and submits each confirmed criterion explicitly. SpecGate does not
interpret headings, numbered lists, filenames, or keywords. After work
creation, that persisted list—not a later recollection of the source
document—is what completion and peer review must cover.

`delivery status --detail` reports the peer-review state as `not_run`,
`passed`, `failed`, or `stale`. A new completion makes an older peer review
stale. This is review evidence, not authority to deliver: a human still
approves or rejects the delivery.

Artifact-backed delivery review also fails closed if the approved artifact or
its frozen policy snapshot cannot be loaded or interpreted. Status then reports
`needs_human_review` with `reason_code: policy_unavailable`; SpecGate does not
silently substitute a weaker evidence policy or call a model. A persisted
snapshot without `evidence_policy` is invalid; only intentional quick work with
no artifact snapshot uses the built-in `attested_ok` policy.
Local artifact approval applies the same fail-closed rule before it records the
human decision. Local gate tasks and their results stay bound to both the
artifact content digest and frozen policy digest. Approval recomputes the
current aggregate from matching gate results, so a prior readiness run cannot
authorize a different policy snapshot.

Every review is bound to the exact completion feedback event it assessed. If a
new completion is submitted but its review has not finished, status reports
`reason_code: delivery_review_outdated`. A human cannot accept the older
review. After the new review, the completion enters a fresh human-decision
cycle; rerunning a review for the same completion cannot silently undo an
existing human decision.

## Per-criterion trust

### Source requirement coverage (Local)

Artifact packages may include `source_criteria`: immutable entries with `id`,
`text`, and `source_path`. Link a work acceptance criterion with exact
`@source:<id>`. `coverage` keeps its existing delivery state and adds
`source_coverage`: `unknown` for legacy artifacts, `unassigned` when a source
entry has no link, `accounted_for` when linked work remains open, and
`delivered` when every linked work item is delivered.

A `deferred_reason` is reviewed as part of approving the immutable snapshot.
It counts as `accounted_for`, never `delivered`.

Local coverage also projects every reviewed source entry: its text and source
path, exact linked work items, and `unassigned`, `in_progress`, `delivered`, or
`deferred` state. Work delivery and source coverage answer different questions.
A delivered work item can coexist with an unassigned source entry because no
work was created for that entry. The projection verifies stored IDs and exact
links; it does not prove the inventory is a complete reading of the source or
that a linked criterion implements the source requirement.

Local mode can additionally pin a verification contract before the first
completion report. It fixes the reviewed commands and repository-relative
working directories for each `@check` binding. A later submission cannot swap
those commands; `--run-checks` reruns them. This does not prove that the tests
cover the requirement, and the commands run without a sandbox. Without a pin,
status explicitly reports `unconfigured`.

Pin only when a human wants that reviewed command agreement to remain stable
across agents or resumes, not as a default quick-work step. Before confirming,
the human reviews the work's current Context Pack digest, each binding and
criterion, and each literal command and checkout-relative directory. A dry-run
checks eligibility but neither writes a pin nor runs its commands. The agreement
is immutable for the work. Changing it requires replacement work.

Local acceptance and rejection require the exact `--review-id` displayed by
the status the human reviewed. A new review requires a new decision against
that ID. See [Local verification contracts](../reference/cli.md#local-resume-and-verification-contracts)
for input, timing, and migration limits.

Each delivery-review criterion can expose a `trust_tier` so humans can see why
SpecGate trusted that row:

- `deterministic` means the criterion had a human-authored
  `verification_binding` and the matching check result drove the verdict.
- `grounded` means the submitted evidence cited a local file path that the CLI
  opened before submission, storing a short excerpt and SHA-256 digest with the
  completion report.
- `peer_reviewed` means a later `coding_agent.peer_reviewed` feedback event
  from a different agent confirmed the criterion. Peer review is optional.

`grounded` and `peer_reviewed` are useful review signals, but they do not
pretend to prove that an automated check exercised the acceptance criterion.

### Git-bound resume receipt

The CLI may attach a `git_receipt` containing repository/branch/revision
metadata, changed-file names, a local digest, and `freshness_scope`. This is self-attested
(`agent_attested`): it detects checkout staleness or drift between report and
submit and gives a new IDE agent a compare-target, but it is not cryptographic
proof of what was built. It contains no source diff or file contents.

When a completion report is refreshed after its branch is pushed, the CLI keeps
the prior receipt base only if it is still an ancestor of `HEAD`; this preserves
the delivered commit range instead of treating an up-to-date tracking branch as
an empty change. Unrelated dirty files remain warnings.

`freshness_scope=shared_repository` compares the stored receipt with the
current repository, branch, base, HEAD, and working-tree digest.
`freshness_scope=local_checkout` requires the same recorded checkout identity
before comparing branch, HEAD, and working-tree digest; it is not portable
provenance. A missing or different checkout identity is unverified, not a stale
comparison, even when branch and HEAD match. Within a comparable checkout, a
match is reported explicitly and a mismatch is a stale warning. Unavailable
non-Git metadata is also reported as unverified rather than guessed.

### Selected Local test observations

An optional Local verification pin can bind a criterion to exact JUnit case
names and explicitly watched verification files. During a confirmed run,
SpecGate observes the command's newly allocated report and bounded endpoint
snapshots. That makes missing, skipped, duplicate, or reported-failing cases
visible; it does not prove the runner was honest, the test was sufficient, or
files did not change and revert between snapshots. Watched-input drift is a
review warning, not an automatic accusation or a different approval entity.

`work checkpoint` records an explicit pickup baseline. It preserves approved
scope while `work resume --since` describes only a compatible Local checkout
delta. Neither checkpoint data nor a current freshness observation changes a
historical evidence result or grants acceptance.

## Automatic policy decides which gates run

Every artifact version freezes a policy snapshot: enabled gates,
required topics and document roles, exact Skill rubric content and digest, and
the approval and evidence policies. Gate runs read only this snapshot on both
the artifact-level and work-item-level paths. Because the snapshot is immutable,
editing a Skill or tightening policy applies to new artifact versions, not
retroactively to in-flight ones.

## Observation limits

An agent report records what the agent says it ran. With `--run-checks`, the
CLI records the command outcome. When a check pins selected JUnit cases,
the CLI also records what the fresh report says about those exact cases. A
passing command can coexist with a missing selected case.

If a criterion requires an end-to-end flow or a real provider call, run that
path and show what it exercised. A unit test with a mock or stub verifies the
behavior of that setup. Review the command, runner, case assertions, and
environment alongside the output to decide whether the evidence covers the
requirement. Git and watched-input observations describe the compared endpoints. A handoff file is an unsigned
request to review evidence and carries no shared decision authority.

## Related

- [Gate catalog](../reference/gates.md)
- [Respond to gate failures](../guides/respond-to-gate-failures.md)
- [Governance and gates](governance-and-gates.md)
- [Evidence reference](../reference/evidence.md)
