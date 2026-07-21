# Gate catalog

Use this reference to look up any gate by its key: what it checks, what
context it reads, who evaluates it, and what its verdicts mean. For how
verdicts are derived, see [How verification works](../concepts/verification.md);
for what to do when one fails, see
[Respond to gate failures](../guides/respond-to-gate-failures.md).

## How to read this catalog

- **Reads** names the context the checker receives. Model-judged gates read
  artifact documents grouped by role; deterministic gates read workflow state.
- **Evaluator** is `deterministic` (code, no model), `model` (the server-side
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

## Readiness Gates

These run when readiness is checked on an artifact (or on a work item's lead
artifact). The resolved automatic policy's `enabled_gates` list decides which of them
run. Platform-model evaluations return a verdict, confidence, an actionable
hint, and an evidence quote; low-confidence pass/fail verdicts are downgraded
to `needs_human_review`. IDE-agent fallback results are agent-attested and use
the same aggregate states.

Local IDE gate tasks and Full model-less IDE gate tasks are frozen against the
artifact snapshot and rubric before the IDE agent evaluates them. A successful
readiness command stays `not_run` until required task results are submitted; it
does not report a pass while semantic work is pending.

| Gate | Checks | Reads |
|---|---|---|
| `scope_clear` | the change is bounded, with explicit non-goals | spec |
| `spec_completeness` | the package covers its required topics (outcomes, acceptance criteria, risks, verification, and any policy-required topics); advisory — caps at `warn` | all documents |
| `acceptance_criteria_verifiable` | every criterion is an observable, testable outcome; vague criteria ("works well", "is fast") are named and restated as observable checks in the hint | spec + verification |
| `acceptance_criteria_edge_cases` | criteria cover failure and edge paths, not only the happy path | spec + verification |
| `success_metric_measurable` | the stated success metric can actually be measured | spec |
| `rollback_plan_present` | a rollback or recovery path is stated | spec + reference |
| `implementation_plan_traceable` | plan tasks trace to criteria and scope | spec + plan + verification |
| `spec_repo_drift` | governed repo docs contradict the approved spec; IDE-agent-executed at pickup (needs repo checkout), warn-only, agent-attested | approved spec + governed repo docs |
| `required_roles` | the document roles the resolved policy requires are present in the package; structural, no model | file list |

Feature attachments with the `gate` or `both` audience are appended to the
acceptance-criteria and scope gates. The resolved automatic policy can bind a team Skill to any
gate; the Skill's text is applied as additional policy.

### `spec_repo_drift` result contract

A `spec_repo_drift` result submitted to `POST /api/v1/gate-tasks/{task_id}/result`
must carry an evidence attestation, else the submission is rejected (`400`):

- `evidence.examined_docs[]` — repo-relative doc paths the agent read (non-empty).
- `evidence.repo_commit` — the checkout commit SHA the examination ran against.
- `findings[]` — zero or more `{doc_path, conflicting_claim, spec_section}`.

The stored readiness run's `evidence_json` preserves both the evidence
attestation and findings for later audit/readback.

The verdict is mapped from the findings, not the submitted state: a valid
attestation with zero findings → `pass`; one or more findings → `warn`. Drift
never maps to `fail`/block — it is only knowable after pickup, so a blocking
verdict would deadlock the flow.

## Delivery review

| Gate | Checks | Reads |
|---|---|---|
| `delivery_review` | each acceptance criterion against the coding agent's claims, evidence citations, and automated check results; overall verdict aggregated deterministically | acceptance criteria + completion report |

The reviewer does not read the git diff; see
[How verification works](../concepts/verification.md#honest-limits). Under
the `corroborated_required` evidence policy, a pass without a matched
merged PR/MR repository event whose `head_sha` matches the latest completion
receipt's `head_revision`, or deterministic bindings for every criterion, is
clamped to `needs_human_review`. CI is not a first-release assurance source.

## Related

- [How verification works](../concepts/verification.md)
- [Governance reference](governance.md)
- [Respond to gate failures](../guides/respond-to-gate-failures.md)
