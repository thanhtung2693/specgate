# How verification works

SpecGate's promise is that a verdict means something: when a gate says `pass`
or a delivery review says `met`, a team should be able to say exactly what was
checked, against what context, and with what confidence. This page explains
how the checkers work and where their limits are. It describes what the code
does in the v0.1 implementation, not an aspiration.

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
   locally captured citation, a reproduced check, peer review, CI, or a
   repository event.
3. **Decision** — whether a human has accepted or rejected the delivery.
4. **Receipt** — which Git revision was recorded with the report. A recorded
   receipt is a comparison target, not proof that it is still the current
   checkout.

A model can review the submitted evidence, but it does not thereby inspect the
code, replace CI, or make the human acceptance decision.

## What a readiness gate sees

When gates run, the platform fetches the artifact's documents and groups them
by role: spec, design, plan, verification, reference. Each gate receives only
the sections relevant to its question. The scope gate reads the spec; the
acceptance-criteria gates read the spec plus verification documents; the
completeness gate reads everything. Feature attachments pinned with the
`gate` audience (bug reproductions, examples, links) are added to the
acceptance-criteria and scope gates. The artifact's work type is included so
a bug fix is not judged by new-feature standards.

SpecGate does not intentionally truncate this bundle. A document that fails
to fetch is skipped rather than aborting the run, and the model's own context
window is the remaining ceiling; typical spec packages are far below it.

Each model-judged gate is an opinionated prompt, not a generic "review this"
request. The acceptance-criteria gate, for example, hunts for untestable phrasing
("works well", "is fast", "handles errors gracefully") and must restate any
offender as an observable check in its hint. When the resolved automatic policy binds
a team Skill to a gate, that Skill's text is appended as team policy, so the
gate judges by your standards, not only its built-in ones.

## How a gate verdict is derived

The model must return structured output: a verdict, a confidence between 0
and 1, an actionable hint, and an evidence quote naming what in the artifact
decided it. Two rules are then applied in code, outside the model:

- a `pass` or `fail` below the confidence threshold (default 0.7) is
  downgraded to `needs_human_review` — an unsure checker never quietly
  decides;
- the completeness gate aggregates per-topic coverage and never exceeds
  `warn` — it advises, it does not block.

The full judgment, including the evidence quote, confidence, and which judge
produced it, is persisted with the gate run.

## What the delivery reviewer sees

The reviewer receives the work item's acceptance criteria and the coding
agent's completion report: one claim per criterion (`satisfied`, `partial`,
`not_done`), automated check results (tests, types, lint), the affected
files, and evidence citations. Its prompt is skeptical by design: default to
`unclear` rather than `met`, do not give the benefit of the doubt, quote the
deciding evidence.

The model judges each criterion; the overall verdict is then deterministic
code:

| Outcome | Condition |
|---|---|
| `pass` | every criterion `met` and no automated check failed |
| `fail` | any criterion `unmet`, or any check failed |
| `needs_human_review` | any criterion `unclear`, no criteria to judge, or low confidence |

The reviewer does not read the git diff. It verifies the agent's account of
the work: every criterion claimed with evidence, claims internally
consistent, automated checks corroborating them. Your tests, CI, and the
human approval step remain the code-level truth.

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

Each delivery-review criterion can expose a `trust_tier` so humans can see why
SpecGate trusted that row:

- `deterministic` means the criterion had a human-authored
  `verification_binding` and the matching check result drove the verdict.
- `grounded` means the submitted evidence cited a local file path that the CLI
  opened before submission, storing a short excerpt and SHA-256 digest with the
  completion report.
- `peer_reviewed` means a later `coding_agent.peer_reviewed` feedback event
  from a different agent confirmed the criterion.

`grounded` and `peer_reviewed` are useful review signals, but they do not
pretend to prove that an automated check exercised the acceptance criterion.

### Git-bound resume receipt

The CLI may attach a `git_receipt` containing repository/branch/revision
metadata, changed-file names, and a local digest. This is self-attested
(`agent_attested`): it detects checkout staleness or drift between report and
submit and gives a new IDE agent a compare-target, but it is not cryptographic
proof of what was built. It contains no source diff or file contents. A merged
PR/MR observes the submitted commit only when its normalized `head_sha` matches
the latest receipt's normalized `head_revision`; do not treat the local digest
as tamper-proof.

When a completion report is refreshed after its branch is pushed, the CLI keeps
the prior receipt base only if it is still an ancestor of `HEAD`; this preserves
the delivered commit range instead of treating an up-to-date tracking branch as
an empty change. Unrelated dirty files remain warnings.

`specgate change status` compares that stored receipt with the current
repository, branch, HEAD, and working-tree digest. A match is reported
explicitly. A mismatch is a stale warning; unavailable Git metadata is reported
as unverified rather than guessed.

## Corroborated evidence

Webhook events from git integrations are matched to the specific work item and
the latest completion receipt:

- a **merged-PR/MR event** records `repository_observed` only when its
  normalized `head_sha` equals the latest completion receipt's normalized
  `git_receipt.head_revision`; missing, mismatched, and stale events do not
  corroborate;
- under the `corroborated_required` evidence policy, a pass without that
  repository observation or deterministic bindings for every criterion is
  clamped to `needs_human_review`. CI is not a first-release assurance source.

## Verification without a server model

The loop still runs when no server-side model is configured, at a recorded
lower trust tier. Readiness gates dispatch as tasks to your IDE coding agent,
which fetches the artifact, applies the same rubric, and submits a result
marked agent-attested; platform-evaluated and agent-attested results are
distinguished in the stored record. Delivery review falls back to mapping the
agent's own per-criterion claims, and the stored review names its origin as
agent-attested rather than a platform model judge.

Gate runs and artifact readiness history expose the evaluator as a
first-class `executor` field (`platform` or `ide_agent`), and agent-attested
runs appear in the history labeled as such; the platform-only filter applies
to the approval aggregate, not to what you can see. One remaining caveat:
delivery reviews mark their origin through the judge name rather than the
same field.

## Automatic policy decides which gates run

Every artifact version freezes a policy snapshot: enabled gates,
required topics and document roles, exact Skill rubric content and digest, and
the approval and evidence policies. Gate runs read only this snapshot on both
the artifact-level and work-item-level paths. Because the snapshot is immutable,
editing a Skill or tightening policy applies to new artifact versions, not
retroactively to in-flight ones.

## Honest limits

Worth knowing when you rely on a verdict:

1. Delivery review verifies the agent's evidence-backed account, not the
   diff. Misreported claims are caught by check contradictions, the skeptical
   default, corroboration policy, and human approval — not by code reading.
2. Readiness gates judge the artifact in isolation. They have no repository
   context, so they cannot assess whether criteria are achievable in your
   codebase.
3. Completeness measures topic coverage, not depth. A shallow mention counts
   as covered, which is why that gate only warns.
4. Corroboration checks that a matching webhook event exists for the work
   item; it does not independently verify the event's contents.
5. A Skill bound to a gate becomes part of that gate's policy verbatim.
   Review team rubrics with the same care as the gates they steer.

## Related

- [Gate catalog](../reference/gates.md)
- [Respond to gate failures](../guides/respond-to-gate-failures.md)
- [Governance and gates](governance-and-gates.md)
- [Evidence reference](../reference/evidence.md)
