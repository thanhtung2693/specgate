# Gate catalog

Use this reference to look up any gate by its key: what it checks, what
context it reads, who evaluates it, and what its verdicts mean. For how
verdicts are derived, see [How verification works](../concepts/verification.md);
for what to do when one fails, see
[Respond to gate failures](../guides/respond-to-gate-failures.md).

## How to read this catalog

- **Reads** names the context the checker receives. LLM gates read artifact
  documents grouped by role; deterministic gates read workflow state.
- **Evaluator** is `deterministic` (code, no model), `LLM` (the server-side
  ops model, or your IDE agent as an agent-attested fallback when no server
  model is configured), or `review` (the delivery reviewer).
- Verdict states are shared across all gates: `pass`, `warn`, `fail`,
  `pending`, `needs_human_review`, `not_applicable`, `not_run`. See
  [Governance reference](governance.md#verdict-states).

## Workflow gates (deterministic)

These run on every work item refresh and never call a model. On quick-route
work (no lead artifact), the first five are persisted as `not_applicable`
because quick work never grows a working spec.

| Gate | Checks | Reads |
|---|---|---|
| `spec_drafted` | a working spec artifact is attached to the work item | work item's lead artifact link |
| `spec_approved` | the attached spec version is human-approved | lead artifact status |
| `no_conflicts` | the working spec has no unresolved conflict with the canonical spec | lead artifact conflict state |
| `knowledge_fresh` | no newer linked knowledge contradicts the working spec | knowledge document timestamps |
| `canonical_spec` | the approved spec has been promoted to the feature's canonical version | feature's canonical pointer |
| `delivery_pack` | an approved Context Pack exists for handoff (stored pack for quick work; assembled on demand for full-route work) | context-pack artifact link |

## Readiness gates (LLM-judged)

These run when readiness is checked on an artifact (or on a work item's lead
artifact). A governance profile's `enabled_gates` list decides which of them
run. Each returns a verdict, confidence, an actionable hint, and an evidence
quote; low-confidence pass/fail verdicts are downgraded to
`needs_human_review`.

| Gate | Checks | Reads |
|---|---|---|
| `scope_clear` | the change is bounded, with explicit non-goals | spec |
| `spec_completeness` | the package covers its required topics (outcomes, acceptance criteria, risks, verification, and any profile-required topics); advisory — caps at `warn` | all documents |
| `acceptance_criteria_verifiable` | every criterion is an observable, testable outcome; vague criteria ("works well", "is fast") are named and restated as observable checks in the hint | spec + verification |
| `acceptance_criteria_edge_cases` | criteria cover failure and edge paths, not only the happy path | spec + verification |
| `success_metric_measurable` | the stated success metric can actually be measured | spec |
| `rollback_plan_present` | a rollback or recovery path is stated | spec + reference |
| `implementation_plan_traceable` | plan tasks trace to criteria and scope | spec + plan + verification |
| `required_roles` | the document roles the profile requires are present in the package; structural, no model | file list |

Feature attachments with the `gate` or `both` audience are appended to the
acceptance-criteria and scope gates. A profile can bind a team Skill to any
gate; the Skill's text is applied as additional policy.

## Delivery review

| Gate | Checks | Reads |
|---|---|---|
| `delivery_review` | each acceptance criterion against the coding agent's claims, evidence citations, and automated check results; overall verdict aggregated deterministically | acceptance criteria + completion report |

The reviewer does not read the git diff; see
[How verification works](../concepts/verification.md#honest-limits). Under
the `corroborated_required` evidence policy, a pass without a matched
merged-PR event is clamped to `needs_human_review`.

## Related

- [How verification works](../concepts/verification.md)
- [Governance reference](governance.md)
- [Respond to gate failures](../guides/respond-to-gate-failures.md)
