# Respond to gate failures

Use this guide when a gate is not green and you need to decide what to do.
Every recipe starts from the same two commands:

```bash
specgate gates status <work-ref>     # current state per gate
specgate gates history <work-ref>    # past runs with hints
```

Each failing or warning run carries a hint written to be acted on; read it
first. To look up what a gate checks, see the
[gate catalog](../reference/gates.md).

## A workflow gate is `pending`

`spec_drafted`, `spec_approved`, `no_conflicts`, `knowledge_fresh`,
`canonical_spec`, and `delivery_pack` are deterministic workflow steps, not
quality judgments. `pending` means the step has not happened yet:

- `spec_drafted` pending — attach or publish a working spec for the item.
- `spec_approved` pending — a human approves the spec
  (`specgate artifact approve <artifact-id>` or the Reviews page).
- `delivery_pack` pending on quick work — generate the pack:
  `specgate work context <work-ref>`.

On quick-route work the first five show `not_applicable` — that is correct,
not a failure; quick work never grows a working spec.

## A readiness gate is `warn` or `fail`

The hint names the problem and, for the acceptance-criteria gate, restates
the offending criterion as an observable check. To fix:

1. Revise the relevant document (the [catalog](../reference/gates.md) shows
   which documents each gate reads).
2. Publish the revision (new artifact version or approved proposal).
3. Re-run: `specgate gates run <work-ref> --yes` (or
   `specgate gates check <artifact-id>` for an artifact).

`spec_completeness` never exceeds `warn`: treat it as a checklist of thin
topics, not a blocker.

## A gate says `needs_human_review`

The checker was not confident enough to decide (below the 0.7 threshold), or
the evidence policy demanded corroboration that is missing. This state is
deliberate: it routes the call to you instead of letting an unsure model
decide. Read the run's hint and evidence, make the judgment yourself, and
either revise the artifact or approve/reject directly.

## Delivery review failed

Get the full verdict, not just the state:

```bash
specgate delivery status <work-ref> --detail
```

The per-criterion breakdown shows `met`, `unmet`, or `unclear` with the
reviewer's reasoning, and `outstanding_md` lists exactly what is missing.

- **`unmet` criteria** — the work (or its evidence) does not satisfy the
  criterion. Fix the implementation, then re-submit:
  `specgate delivery submit <work-ref> --file completion.json`.
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
merged-PR webhook event. Either merge the PR (the event arrives through the
git integration) and re-run the review, or have a human approve on the
strength of the existing evidence.

## Related

- [Gate catalog](../reference/gates.md)
- [How verification works](../concepts/verification.md)
- [CLI workflow](cli-workflow.md)
