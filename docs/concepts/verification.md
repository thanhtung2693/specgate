# How verification works

SpecGate's promise is that a verdict means something: when a gate says `pass`
or a delivery review says `met`, a team should be able to say exactly what was
checked, against what context, and with what confidence. This page explains
how the checkers work and where their limits are. It describes what the code
does today, not an aspiration.

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

Each LLM gate is an opinionated prompt, not a generic "review this" request.
The acceptance-criteria gate, for example, hunts for untestable phrasing
("works well", "is fast", "handles errors gracefully") and must restate any
offender as an observable check in its hint. When a governance profile binds
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

## Corroborated evidence

Webhook events from git integrations are matched to the specific work item
and raise the trust tier of a review:

- a **merged-PR event** is independently-corroborated evidence; under the
  `corroborated_required` evidence policy, a pass without one is clamped to
  `needs_human_review`;
- a **CI-passed event** is recorded as `ci_verified` evidence, stronger than
  agent-reported, but it does not by itself satisfy the corroboration
  requirement.

## Verification without a server model

The loop still runs when no server-side model is configured, at a recorded
lower trust tier. Readiness gates dispatch as tasks to your IDE coding agent,
which fetches the artifact, applies the same rubric, and submits a result
marked agent-attested; platform-evaluated and agent-attested results are
distinguished in the stored record. Delivery review falls back to mapping the
agent's own per-criterion claims, and the stored review names its origin as
agent-attested rather than an LLM judge.

One caveat: the trust marking is currently per-path — an explicit trust field
on gate-task results, the judge name on delivery reviews — not yet a single
uniform field across every surface.

## Profiles decide which gates run

Every artifact version freezes a governance profile snapshot: enabled gates,
required topics and document roles, Skill bindings, and the approval and
evidence policies. Gate runs read this snapshot on both the artifact-level
and work-item-level paths. Because the snapshot is immutable, tightening team
policy applies to new artifact versions, not retroactively to in-flight ones.

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
- [Customize governance](../guides/customize-governance.md)
- [Governance and gates](governance-and-gates.md)
- [Evidence reference](../reference/evidence.md)
