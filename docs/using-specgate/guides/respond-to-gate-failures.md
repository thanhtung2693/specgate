# Respond to gate failures

Use this guide when a gate needs attention. Start by asking SpecGate what
happened and what changed over time:

```bash
specgate gates status <work-ref>     # current state per gate
specgate gates history <work-ref>    # past runs with hints
```

Read the hint on the latest warning or failure first; it should tell you what to
do next. To look up the inputs and purpose of a gate, see the
[gate catalog](../reference/gates.md).

## A workflow gate is `pending`

`spec_drafted`, `spec_approved`, `no_conflicts`, `knowledge_fresh`,
`canonical_spec` is a deterministic workflow step, not
quality judgments. `pending` means the step has not happened yet:

- `spec_drafted` pending — attach or publish a working spec for the item.
- `spec_approved` pending — a human approves the spec and explicit work handoff
  (`specgate --yes change approve <artifact-id> --title <title> --ac <criterion>`),
  or uses the Reviews page for the expert artifact-only decision.
On quick-route work the first five show `not_applicable` — that is correct,
not a failure; quick work never grows a working spec.

## A readiness gate is `warn` or `fail`

The hint names the problem and, for the acceptance-criteria gate, restates
the offending criterion as an observable check. To fix:

1. Revise the relevant document (the [catalog](../reference/gates.md) shows
   which documents each gate reads).
2. Publish the revision as a new artifact version.
3. Re-run: `specgate gates run <work-ref> --yes` (or
   `specgate gates check <artifact-id>` for an artifact).

`spec_completeness` never exceeds `warn`: treat it as a checklist of thin
topics, not a blocker.

## A gate says `needs_human_review`

The checker was not confident enough to decide (below the 0.7 threshold), or
the evidence policy required independent evidence that is missing. This is an
intentional stop, not a broken gate. Read the hint and evidence, then revise the
artifact or make the approval decision yourself.

For delivery review, `reason_code: policy_unavailable` means SpecGate could not
load or interpret the frozen governing policy. Restore the approved artifact
and policy snapshot, then retry. Do not treat this as an ordinary failed
criterion or work around it by configuring a model; the review stopped before
model judgment.

`reason_code: delivery_review_outdated` means a newer completion was stored
after the displayed review. Run `specgate delivery review <work-ref>` before a
human decision. SpecGate refuses to apply the older review to the newer Git
receipt.

## Delivery review failed

Start with the full verdict, not only the status name:

```bash
specgate delivery status <work-ref> --detail
```

The status first separates evidence, assurance, human decision, and the recorded
Git receipt. The per-criterion breakdown then shows `met`, `unmet`, or `unclear`
with the reviewer's reasoning, and `outstanding_md` lists exactly what is
missing.

- **`unmet` criteria** — the work (or its evidence) does not satisfy the
  criterion. Fix the implementation, update the completion report at the exact
  path returned by `specgate delivery report <work-ref> --init --json`, then run
  `specgate change submit <work-ref> --file "$COMPLETION_PATH"` after assigning
  that path to `COMPLETION_PATH`.
- **`unclear` criteria** — the report under-claimed: no claim for the
  criterion, or evidence too thin to judge. Improve the completion report
  (one claim per criterion, concrete evidence paths) rather than the code.
  If the review echoes criterion ids such as `ac-0`, reuse those ids in the
  next report so claims match criteria.
- **A check failed** — any failing automated check (tests, types, lint)
  fails the review regardless of claims. Fix and re-run the check first.

Failed criteria carry into the next Context Pack, so the next handoff
targets the gaps.

## The verdict passed but was clamped to `needs_human_review`

Under the `corroborated_required` evidence policy, a pass needs a matched
merged PR/MR repository event whose `head_sha` matches the latest completion
receipt's `head_revision`, or deterministic bindings for every criterion.
Either merge the submitted commit (the event arrives through the git
integration), add canonical check bindings and re-run the review, or have a
human approve on the strength of the existing evidence. CI is not a
first-release assurance source.

## Related

- [Gate catalog](../reference/gates.md)
- [How verification works](../concepts/verification.md)
- [CLI workflow](cli-workflow.md)
