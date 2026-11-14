# Respond to gate failures

Read the saved readiness results for the artifact you are preparing:

```bash
specgate gates results <artifact-id> --json
```

Read the hint on the latest warning or failure first; it should tell you what to
do next. To look up the inputs and purpose of a gate, see the
[gate catalog](../reference/gates.md).

## A readiness gate is `warn` or `fail`

Use the recorded hint and affected document to prepare a revision:

1. Revise the relevant document (the [catalog](../reference/gates.md) shows
   which documents each gate reads).
2. Publish the revision as a new artifact version.
3. Run `specgate gates check <artifact-id>` on the new version and have your
   coding agent complete the resulting [readiness tasks](cli-workflow.md).

## A gate says `needs_human_review`

Read the rubric result for unresolved scope or the review for evidence
that needs human judgment.

For delivery review, `reason_code: policy_unavailable` means SpecGate could not
load or interpret the frozen governing policy. Check that your CLI supports
the artifact's policy and use a compatible Local backup if recovery is needed.
See [Operate SpecGate](operate-specgate.md#operate-local-cli).

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
- **`unclear` criteria** — inspect the reported gap. Supply the missing
  criterion claim, concrete evidence path, or bound check as appropriate.
  If the review echoes criterion ids such as `ac-0`, reuse those ids in the
  next report so claims match criteria.
- **A check failed** — any failing automated check (tests, types, lint)
  fails the review regardless of claims. Fix and re-run the check first.

Failed criteria carry into the next Context Pack, so the next handoff
targets the gaps.

## Local assurance evidence is missing or stale

For a report-enabled Local check, read `change status --json` and fix the
specific reported condition: write the selected JUnit cases to the transient
path supplied during `--run-checks`; correct duplicate, skipped, malformed, or
unselected failing cases; or review an explicitly changed watched input. Do
not copy an old XML file into a new completion or change the pin after a report;
pin changes require replacement work.

If an enhanced `basis_digest` conflicts, rerun status, inspect the changed
evidence or checkout observation, then ask the human to review the new result.
Never substitute a fresh digest into an old approval. A wrong checkpoint or an
incompatible worktree is informationally unavailable/noncomparable: select the
correct work-local checkpoint or continue without a baseline. Export refusal
means the v1 format would lose Local assurance records; keep using the Local
store or restore its disclosed pre-upgrade backup in a separately chosen
location.

## Related

- [Gate catalog](../reference/gates.md)
- [How verification works](../concepts/verification.md)
- [CLI workflow](cli-workflow.md)
