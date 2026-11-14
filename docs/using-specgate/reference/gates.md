# Gate catalog

Use this reference to look up any gate by its key: what it checks, what
context it reads, who evaluates it, and what its verdicts mean. For how
verdicts are derived, see [How verification works](../concepts/verification.md);
for what to do when one fails, see
[Respond to gate failures](../guides/respond-to-gate-failures.md).

## How to read this catalog

- **Reads** names the context the checker receives. Semantic readiness tasks read
  artifact documents grouped by role; deterministic gates read workflow state.
- **Evaluator** identifies deterministic code, the IDE agent, or a delivery review.
- Verdict states are shared across all gates: `pass`, `warn`, `fail`,
  `needs_human_review`, `not_applicable`, `not_run`. See
  [Governance reference](governance.md#verdict-states).

## Readiness gates

The CLI runs structural checks on an artifact and dispatches semantic tasks
to your coding agent. Each task uses the frozen artifact snapshot and rubric.
While required tasks are open, readiness stays `not_run`. Semantic results
are agent-attested. Quick work starts from explicit criteria and skips artifact
readiness.

| Gate | Checks | Reads |
|---|---|---|
| `scope_clear` | the change is bounded, with explicit non-goals | spec |
| `spec_completeness` | the package covers scope, outcomes, acceptance criteria, and verification | all mapped documents |
| `acceptance_criteria_verifiable` | every criterion is an observable, testable outcome; vague criteria ("works well", "is fast") are named and restated as observable checks in the hint | spec + plan + optional verification |
| `spec_repo_drift` | governed repo docs contradict the approved spec; IDE-agent-executed at pickup (needs repo checkout), warn-only, agent-attested | approved spec + governed repo docs |
| `required_roles_present` | every document role the resolved policy's `required_roles` lists is present in the package; structural, no model. A document's identity is its `path` and `role` together, so one source may be mapped under several roles rather than a new document being written. Failure blocks approval | file list |
| `has_documents` | the package contains at least one immutable document; structural, no model | file list |

Add project instructions to the four semantic rubrics before publication
with [Local project instructions](../guides/customize-governance.md#add-local-project-instructions).

### `spec_repo_drift` result contract

A `spec_repo_drift` result passed to `specgate gates tasks submit-result`
must carry an evidence attestation:

- `evidence.examined_docs[]` — repo-relative doc paths the agent read (non-empty).
- `evidence.repo_commit` — the checkout commit SHA the examination ran against.
- `findings[]` — zero or more `{doc_path, conflicting_claim, spec_section}`.

The stored readiness run's `evidence_json` preserves both the evidence
attestation and findings for later audit/readback.

The CLI derives the verdict from the findings after approval. An empty list
produces `pass`. One or more findings produces `warn`.

## Delivery review

| Gate | Checks | Reads |
|---|---|---|
| `delivery_review` | each acceptance criterion against the coding agent's claims, evidence citations, and automated check results; overall verdict aggregated deterministically | acceptance criteria + completion report |

The automated review uses the submitted evidence and stored criteria. A human
can also review the Git diff, CI output, and test scope before deciding.
See [How verification works](../concepts/verification.md#observation-limits).

## Related

- [How verification works](../concepts/verification.md)
- [Governance reference](governance.md)
- [Respond to gate failures](../guides/respond-to-gate-failures.md)
