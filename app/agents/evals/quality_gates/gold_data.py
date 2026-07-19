"""Quality-gate eval gold-data format.

Defines per-gate gold fixtures for the single-verdict model-judged gates evaluated by
``specgate_agents.governance.quality_gates.judge`` (every gate in ``ALL_LLM_GATES``
except ``spec_completeness``, whose per-topic coverage semantics have their own
suite in ``tests/governance/test_completeness.py``). Each fixture carries a compact
artifact body that should clearly pass or clearly fail/warn the gate it
targets, plus the resolved ``GateState`` a faithful judge should land on.

CI runs these against a fixture judge that returns the fixture's intended
verdict/confidence; a live calibration test runs the real judge. The expected
state is derived from ``resolve_gate_state`` so the offline path genuinely
exercises the low-confidence-pass downgrade rather than asserting a tautology.

No keyword matching over artifact text lives here — the gate decision always
comes from the model (see agents AGENTS.md). The fixtures only describe what a
correct judge ought to conclude.
"""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from typing import Any

from pydantic import BaseModel, Field

from specgate_agents.governance.quality_gates.judge import (
    AC_EDGE_CASES_GATE,
    AC_VERIFIABLE_GATE,
    IMPLEMENTATION_TRACEABLE_GATE,
    ROLLBACK_PLAN_GATE,
    SCOPE_CLEAR_GATE,
    SUCCESS_METRIC_GATE,
    GateEvaluation,
    GateState,
    evaluate_acceptance_criteria_edge_cases,
    evaluate_acceptance_criteria_verifiable,
    evaluate_implementation_plan_traceable,
    evaluate_rollback_plan,
    evaluate_scope_clear,
    evaluate_success_metric_measurable,
    resolve_gate_state,
)

# Maps a gate-name constant to its single-artifact evaluate_* entry point.
# Keys are structural gate-name constants (not user content), so this dispatch
# is allowed under the multi-language/LLM rule. Fixtures target one gate each,
# so we call the one function the fixture exercises instead of the full bundle.
GATE_EVALUATORS: dict[str, Callable[..., Awaitable[GateEvaluation]]] = {
    ROLLBACK_PLAN_GATE: evaluate_rollback_plan,
    AC_EDGE_CASES_GATE: evaluate_acceptance_criteria_edge_cases,
    AC_VERIFIABLE_GATE: evaluate_acceptance_criteria_verifiable,
    SUCCESS_METRIC_GATE: evaluate_success_metric_measurable,
    SCOPE_CLEAR_GATE: evaluate_scope_clear,
    IMPLEMENTATION_TRACEABLE_GATE: evaluate_implementation_plan_traceable,
}


class GateEvalCase(BaseModel):
    """One quality-gate eval fixture: an artifact body aimed at a single gate.

    ``judge_verdict`` / ``judge_confidence`` are what a faithful judge should
    return for ``artifact_md``; ``expected_state`` is the resolved gate state
    after the deterministic confidence downgrade. They are kept consistent by
    construction (see :func:`_case`), so a regression in ``resolve_gate_state``
    surfaces as a fixture mismatch.
    """

    case_id: str
    gate: str = Field(description="Gate-name constant this fixture targets.")
    description: str
    artifact_md: str = Field(description="Compact PRD/spec/impl markdown to judge.")
    judge_verdict: GateState = Field(description="Raw verdict a faithful judge should return.")
    judge_confidence: float = Field(ge=0.0, le=1.0, description="Confidence of that verdict.")
    expected_state: GateState = Field(description="Resolved state after the confidence downgrade.")


def _case(
    *,
    case_id: str,
    gate: str,
    description: str,
    artifact_md: str,
    judge_verdict: GateState,
    judge_confidence: float,
) -> GateEvalCase:
    """Build a case, deriving ``expected_state`` from the judge's own logic."""
    return GateEvalCase(
        case_id=case_id,
        gate=gate,
        description=description,
        artifact_md=artifact_md,
        judge_verdict=judge_verdict,
        judge_confidence=judge_confidence,
        expected_state=resolve_gate_state(judge_verdict, judge_confidence),
    )


# ---------------------------------------------------------------------------
# Gold fixtures — at least one clear pass and one clear fail/warn per gate.
# ---------------------------------------------------------------------------

_ROLLBACK_PASS = """\
## Spec: Checkout retry

### Rollback plan
The change ships behind the `checkout_retry` feature flag (default off). To roll
back: flip the flag off in LaunchDarkly — no deploy required. The retry counter
column is additive and nullable, so no data migration cleanup is needed. Trigger:
roll back if payment error rate exceeds 2% for 10 minutes after enable.
"""

_ROLLBACK_FAIL = """\
## Spec: Checkout retry

POST /checkout now retries failed payments up to 3 times with exponential
backoff. The retry counter is stored on the order row.

### Acceptance criteria
- A failed payment is retried up to 3 times before surfacing an error.
"""

_AC_PASS = """\
## PRD: Supplier bulk import

### Acceptance criteria
- A valid CSV of 1..10,000 rows imports all rows.
- An empty file is rejected with a clear "no rows" message.
- A row with a duplicate supplier key is skipped and reported, not failed.
- If the import is retried after a partial failure, already-imported rows are
  not duplicated (idempotent by supplier key).
- A non-admin user cannot trigger an import (403).
- Two concurrent imports of the same file do not double-insert.
"""

_AC_WARN = """\
## PRD: Supplier bulk import

### Acceptance criteria
- The user can upload a CSV file.
- Suppliers appear in the list after import.
"""

_AC_VERIFIABLE_PASS = """\
## PRD: Password reset

### Acceptance criteria
- Submitting a registered email sends a reset link within 60 seconds (assert via
  the mail queue).
- The reset link expires exactly 30 minutes after issue; a later click shows
  "link expired".
- A used reset link cannot be reused — a second submission returns HTTP 410.
- An unregistered email returns the same generic success message (no account
  enumeration).
"""

_AC_VERIFIABLE_WARN = """\
## PRD: Password reset

### Acceptance criteria
- Password reset should work smoothly.
- The experience should feel secure and reliable.
- Users should not get confused.
"""

_METRIC_PASS = """\
## PRD: Checkout latency

### Success metrics
- p95 checkout-submit latency < 300 ms within 7 days of rollout, measured via
  the `checkout.submit.latency` Datadog monitor on the prod dashboard.
- Payment success rate stays >= 98.5% week-over-week, verified in the
  payments Looker board.
"""

_METRIC_FAIL = """\
## PRD: Checkout latency

### Success metrics
- Make checkout feel faster.
- Improve the overall experience and reduce friction.
"""

_SCOPE_PASS = """\
## Spec: Notification preferences

### Scope
In scope: a per-user preferences page covering email and in-app channels;
storage of preferences in the `user_prefs` table; the notification dispatcher
reads these flags before sending.

Out of scope: SMS and push channels; admin-level org-wide defaults; migrating
existing implicit opt-ins (handled in a follow-up).

Affected systems: web app, notification-service, user-prefs DB.
"""

_SCOPE_WARN = """\
## Spec: Notification preferences

We will add notification preferences. This may include email and possibly SMS,
depending on future decisions. Other channels could be added later as needed.
"""

_IMPL_PASS = """\
## Spec: Saved searches

### Acceptance criteria
- AC1: A user can save the current search with a name.
- AC2: Saved searches appear in a dropdown and re-run on select.

### Implementation plan
- T1 (covers AC1): add POST /saved-searches storing name + query JSON.
- T2 (covers AC1): add "Save search" button wired to T1.
- T3 (covers AC2): add GET /saved-searches and the dropdown that re-runs the
  stored query.
"""

_IMPL_FAIL = """\
## Spec: Saved searches

### Acceptance criteria
- AC1: A user can save the current search with a name.
- AC2: Saved searches appear in a dropdown and re-run on select.

### Implementation plan
- Refactor the analytics pipeline.
- Add a new caching layer.
- Upgrade the search index library.
"""


GOLD_FIXTURES: list[GateEvalCase] = [
    _case(
        case_id="rollback-pass",
        gate=ROLLBACK_PLAN_GATE,
        description="Flag-gated change with a clear backout, no data cleanup, explicit trigger.",
        artifact_md=_ROLLBACK_PASS,
        judge_verdict="pass",
        judge_confidence=0.92,
    ),
    _case(
        case_id="rollback-fail",
        gate=ROLLBACK_PLAN_GATE,
        description="Spec ships a schema write with no rollback section or trigger.",
        artifact_md=_ROLLBACK_FAIL,
        judge_verdict="fail",
        judge_confidence=0.88,
    ),
    _case(
        case_id="ac-edge-cases-pass",
        gate=AC_EDGE_CASES_GATE,
        description="ACs cover empty input, duplicates, idempotency, permissions, concurrency.",
        artifact_md=_AC_PASS,
        judge_verdict="pass",
        judge_confidence=0.87,
    ),
    _case(
        case_id="ac-edge-cases-warn",
        gate=AC_EDGE_CASES_GATE,
        description="ACs describe only the happy path; no edge cases for a risky import.",
        artifact_md=_AC_WARN,
        judge_verdict="warn",
        judge_confidence=0.8,
    ),
    _case(
        case_id="ac-verifiable-pass",
        gate=AC_VERIFIABLE_GATE,
        description="ACs state objective, observable pass/fail conditions.",
        artifact_md=_AC_VERIFIABLE_PASS,
        judge_verdict="pass",
        judge_confidence=0.89,
    ),
    _case(
        case_id="ac-verifiable-warn",
        gate=AC_VERIFIABLE_GATE,
        description="ACs are subjective and untestable ('work smoothly', 'feel secure').",
        artifact_md=_AC_VERIFIABLE_WARN,
        judge_verdict="warn",
        judge_confidence=0.83,
    ),
    _case(
        case_id="success-metric-pass",
        gate=SUCCESS_METRIC_GATE,
        description="Metrics name signal, target, timeframe, and verification source.",
        artifact_md=_METRIC_PASS,
        judge_verdict="pass",
        judge_confidence=0.9,
    ),
    _case(
        case_id="success-metric-fail",
        gate=SUCCESS_METRIC_GATE,
        description="Metrics are unmeasurable aspirations ('make it faster').",
        artifact_md=_METRIC_FAIL,
        judge_verdict="fail",
        judge_confidence=0.9,
    ),
    _case(
        case_id="scope-clear-pass",
        gate=SCOPE_CLEAR_GATE,
        description="Scope states in/out of scope and affected systems explicitly.",
        artifact_md=_SCOPE_PASS,
        judge_verdict="pass",
        judge_confidence=0.88,
    ),
    _case(
        case_id="scope-clear-warn",
        gate=SCOPE_CLEAR_GATE,
        description="Scope is hedged ('may include', 'depends on future decisions').",
        artifact_md=_SCOPE_WARN,
        judge_verdict="warn",
        judge_confidence=0.82,
    ),
    _case(
        case_id="impl-traceable-pass",
        gate=IMPLEMENTATION_TRACEABLE_GATE,
        description="Each task names the AC it covers; every AC has coverage.",
        artifact_md=_IMPL_PASS,
        judge_verdict="pass",
        judge_confidence=0.86,
    ),
    _case(
        case_id="impl-traceable-fail",
        gate=IMPLEMENTATION_TRACEABLE_GATE,
        description="Tasks are orphan refactors that map to none of the ACs.",
        artifact_md=_IMPL_FAIL,
        judge_verdict="fail",
        judge_confidence=0.85,
    ),
    _case(
        case_id="rollback-low-confidence-pass-escalates",
        gate=ROLLBACK_PLAN_GATE,
        description=(
            "Judge leans pass but is unsure — a low-confidence pass must downgrade "
            "to needs_human_review, exercising resolve_gate_state."
        ),
        artifact_md=_ROLLBACK_PASS,
        judge_verdict="pass",
        judge_confidence=0.5,
    ),
]


def score_gate_evaluations(
    pairs: list[tuple[GateEvaluation, GateEvalCase]],
) -> tuple[float, list[str]]:
    """Score resolved gate evaluations against their gold expectations.

    ``pairs`` couples each :class:`GateEvaluation` to the fixture it was run
    for (``GateEvaluation`` carries no case id, so pairing is the caller's
    responsibility — keep the ordering explicit). Returns a ``(score 0..1,
    diagnostics)`` pair: the fraction of fixtures whose resolved ``state``
    matches ``expected_state``, plus a diagnostic line per mismatch.
    """
    diags: list[str] = []
    if not pairs:
        return 0.0, ["no evaluations to score"]

    passed = 0
    for evaluation, case in pairs:
        gate_ok = evaluation.gate == case.gate
        state_ok = evaluation.state == case.expected_state
        if gate_ok and state_ok:
            passed += 1
            continue
        if not gate_ok:
            diags.append(f"{case.case_id}: gate={evaluation.gate!r}, expected {case.gate!r}")
        if not state_ok:
            diags.append(
                f"{case.case_id}: state={evaluation.state!r}, expected {case.expected_state!r}"
            )

    return passed / len(pairs), diags


async def run_gate_for_case(case: GateEvalCase, *, model: Any) -> GateEvaluation:
    """Run the single gate a fixture targets against the supplied judge model."""
    evaluate = GATE_EVALUATORS[case.gate]
    return await evaluate(case.artifact_md, model=model)
