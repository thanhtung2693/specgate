"""Tests for the post-build delivery reviewer.

Mirrors the quality-gate judge contract: the per-criterion verdicts come from the
model, the overall Pass/Fail is enforced deterministically (pass only if every
criterion is met AND no check failed) with the same low-confidence downgrade.
"""

from __future__ import annotations

import pytest

from specgate_agents.governance.quality_gates.delivery_review import (
    DELIVERY_REVIEW_GATE,
    CriterionReview,
    DeliveryReviewJudgment,
    _resolve_overall,
    derive_review_from_claims,
    review_delivery,
)


class _FakeStructured:
    def __init__(self, decision: DeliveryReviewJudgment) -> None:
        self._decision = decision

    async def ainvoke(self, messages, config=None):  # noqa: ANN001, ANN201
        return self._decision


class _FakeModel:
    def __init__(self, decision: DeliveryReviewJudgment) -> None:
        self._decision = decision

    def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
        return _FakeStructured(self._decision)


AC = [
    {"id": "ac-1", "text": "Import disabled when disconnected"},
    {"id": "ac-2", "text": "Show CTA"},
]


def _payload(checks=None, criteria=None):
    return {"summary": "done", "checks": checks or [], "criteria": criteria or []}


@pytest.mark.asyncio
async def test_all_met_with_green_checks_passes() -> None:
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[
                CriterionReview(criterion_id="ac-1", verdict="met"),
                CriterionReview(criterion_id="ac-2", verdict="met"),
            ],
            confidence=0.9,
        )
    )
    review = await review_delivery(
        acceptance_criteria=AC,
        completed_payload=_payload(checks=[{"name": "tests", "status": "pass"}]),
        model=model,
    )
    assert review.gate == DELIVERY_REVIEW_GATE
    assert review.state == "pass"


@pytest.mark.asyncio
async def test_one_unmet_criterion_fails() -> None:
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[
                CriterionReview(criterion_id="ac-1", verdict="met"),
                CriterionReview(criterion_id="ac-2", verdict="unmet", why="CTA missing"),
            ],
            confidence=0.9,
        )
    )
    review = await review_delivery(
        acceptance_criteria=AC, completed_payload=_payload(), model=model
    )
    assert review.state == "fail"


@pytest.mark.asyncio
async def test_a_failing_check_fails_even_when_criteria_met() -> None:
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[
                CriterionReview(criterion_id="ac-1", verdict="met"),
                CriterionReview(criterion_id="ac-2", verdict="met"),
            ],
            confidence=0.9,
        )
    )
    review = await review_delivery(
        acceptance_criteria=AC,
        completed_payload=_payload(
            checks=[{"name": "tests", "status": "fail", "detail": "2 failing"}]
        ),
        model=model,
    )
    assert review.state == "fail"


@pytest.mark.asyncio
async def test_low_confidence_pass_escalates_to_needs_human_review() -> None:
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[
                CriterionReview(criterion_id="ac-1", verdict="met"),
                CriterionReview(criterion_id="ac-2", verdict="met"),
            ],
            confidence=0.3,
        )
    )
    review = await review_delivery(
        acceptance_criteria=AC, completed_payload=_payload(), model=model
    )
    assert review.state == "needs_human_review"


@pytest.mark.asyncio
async def test_unclear_criterion_needs_human_review() -> None:
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[
                CriterionReview(criterion_id="ac-1", verdict="met"),
                CriterionReview(criterion_id="ac-2", verdict="unclear", why="thin evidence"),
            ],
            confidence=0.9,
        )
    )
    review = await review_delivery(
        acceptance_criteria=AC, completed_payload=_payload(), model=model
    )
    assert review.state == "needs_human_review"
    # the checks are echoed onto the verdict for persistence/UI
    assert isinstance(review.checks, list)


# ── Slice B: evidence-policy clamp ─────────────────────────────────────────

ALL_MET = [
    CriterionReview(criterion_id="ac-1", verdict="met"),
    CriterionReview(criterion_id="ac-2", verdict="met"),
]
GREEN_CHECKS = [{"name": "tests", "status": "pass"}]


def test_resolve_overall_corroborated_required_no_corroborated_clamps_pass() -> None:
    """corroborated_required + no corroborated evidence → pass clamped to needs_human_review."""
    state = _resolve_overall(
        ALL_MET, GREEN_CHECKS, confidence=0.9,
        evidence_policy="corroborated_required", has_corroborated=False,
    )
    assert state == "needs_human_review"


def test_resolve_overall_corroborated_required_with_corroborated_allows_pass() -> None:
    """corroborated_required + corroborated evidence present → pass is allowed."""
    state = _resolve_overall(
        ALL_MET, GREEN_CHECKS, confidence=0.9,
        evidence_policy="corroborated_required", has_corroborated=True,
    )
    assert state == "pass"


def test_resolve_overall_attested_ok_no_clamp() -> None:
    """attested_ok (default) → no clamp regardless of has_corroborated."""
    state = _resolve_overall(
        ALL_MET, GREEN_CHECKS, confidence=0.9,
        evidence_policy="attested_ok", has_corroborated=False,
    )
    assert state == "pass"


def test_resolve_overall_corroborated_required_fail_unchanged() -> None:
    """Clamp only affects pass; fail stays fail even with no corroborated evidence."""
    from specgate_agents.governance.quality_gates.delivery_review import CriterionReview
    state = _resolve_overall(
        [CriterionReview(criterion_id="ac-1", verdict="unmet")], [], confidence=0.9,
        evidence_policy="corroborated_required", has_corroborated=False,
    )
    assert state == "fail"


def test_resolve_overall_corroborated_required_already_needs_human_unchanged() -> None:
    """Clamp is a no-op when base is already needs_human_review."""
    state = _resolve_overall(
        [CriterionReview(criterion_id="ac-1", verdict="unclear")], [], confidence=0.9,
        evidence_policy="corroborated_required", has_corroborated=False,
    )
    assert state == "needs_human_review"


@pytest.mark.asyncio
async def test_review_delivery_threads_evidence_policy_and_has_corroborated() -> None:
    """review_delivery accepts evidence_policy + has_corroborated and threads them to clamp."""
    model = _FakeModel(
        DeliveryReviewJudgment(criteria=list(ALL_MET), confidence=0.9)
    )
    review = await review_delivery(
        acceptance_criteria=AC,
        completed_payload=_payload(checks=list(GREEN_CHECKS)),
        model=model,
        evidence_policy="corroborated_required",
        has_corroborated=False,
    )
    assert review.state == "needs_human_review"


@pytest.mark.asyncio
async def test_review_delivery_injects_rubric_into_prompt() -> None:
    """A bound review-impl Skill prompt is appended to the delivery-review judge prompt."""
    captured: list[str] = []

    class _CapStructured:
        async def ainvoke(self, messages, config=None):  # noqa: ANN001, ANN201
            captured.extend(str(getattr(m, "content", m)) for m in messages)
            return DeliveryReviewJudgment(criteria=list(ALL_MET), confidence=0.9)

    class _CapModel:
        def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
            return _CapStructured()

    await review_delivery(
        acceptance_criteria=AC,
        completed_payload=_payload(),
        model=_CapModel(),
        rubric="DELIVERY_RUBRIC_SENTINEL applies {placeholder}.",
    )
    joined = "\n".join(captured)
    assert "DELIVERY_RUBRIC_SENTINEL" in joined
    assert "{placeholder}" in joined  # injected post-format; braces survive


# --- derive_review_from_claims (no-platform-model path) ---

_ACS = [{"id": "ac_1", "text": "A"}, {"id": "ac_2", "text": "B"}]


def test_derive_all_satisfied_passes() -> None:
    r = derive_review_from_claims(
        acceptance_criteria=_ACS,
        completed_payload={
            "criteria": [
                {"criterion_id": "ac_1", "claim": "satisfied"},
                {"criterion_id": "ac_2", "claim": "satisfied"},
            ],
            "checks": [{"name": "tests", "status": "pass"}],
        },
    )
    assert r.state == "pass"
    assert r.gate == DELIVERY_REVIEW_GATE
    assert r.judge_model == "agent_attested"
    assert len(r.criteria) == 2


def test_derive_not_done_fails() -> None:
    r = derive_review_from_claims(
        acceptance_criteria=_ACS,
        completed_payload={
            "criteria": [
                {"criterion_id": "ac_1", "claim": "satisfied"},
                {"criterion_id": "ac_2", "claim": "not_done"},
            ]
        },
    )
    assert r.state == "fail"


def test_derive_partial_needs_human_review() -> None:
    r = derive_review_from_claims(
        acceptance_criteria=_ACS,
        completed_payload={
            "criteria": [
                {"criterion_id": "ac_1", "claim": "satisfied"},
                {"criterion_id": "ac_2", "claim": "partial"},
            ]
        },
    )
    assert r.state == "needs_human_review"


def test_derive_underreported_criterion_needs_human_review() -> None:
    # ac_2 has no claim in the report → unclear → needs_human_review (no silent pass).
    r = derive_review_from_claims(
        acceptance_criteria=_ACS,
        completed_payload={"criteria": [{"criterion_id": "ac_1", "claim": "satisfied"}]},
    )
    assert r.state == "needs_human_review"


def test_derive_failing_check_fails_even_when_all_satisfied() -> None:
    r = derive_review_from_claims(
        acceptance_criteria=_ACS,
        completed_payload={
            "criteria": [
                {"criterion_id": "ac_1", "claim": "satisfied"},
                {"criterion_id": "ac_2", "claim": "satisfied"},
            ],
            "checks": [{"name": "tests", "status": "fail"}],
        },
    )
    assert r.state == "fail"


def test_derive_corroborated_required_clamps_pass() -> None:
    r = derive_review_from_claims(
        acceptance_criteria=_ACS,
        completed_payload={
            "criteria": [
                {"criterion_id": "ac_1", "claim": "satisfied"},
                {"criterion_id": "ac_2", "claim": "satisfied"},
            ]
        },
        evidence_policy="corroborated_required",
        has_corroborated=False,
    )
    assert r.state == "needs_human_review"


def test_derive_review_matches_claims_by_text_when_ids_differ() -> None:
    """--init prefills registry table UUIDs while the CR's embedded criteria
    synthesize ac-N ids; claims must still correlate via exact text."""
    review = derive_review_from_claims(
        acceptance_criteria=[
            {"id": "ac-0", "text": "The env example documents the chat key."},
            {"id": "ac-1", "text": "The README explains the chat panel."},
        ],
        completed_payload={
            "criteria": [
                {
                    "criterion_id": "90bce791-0ae9-4e8e-a15b-c34abd4636e4",
                    "text": "The env example documents the chat key.",
                    "claim": "satisfied",
                },
                {
                    "criterion_id": "7ab1321c-f225-40d9-be31-59bd45304fad",
                    "text": "The README explains the chat panel.",
                    "claim": "satisfied",
                },
            ],
            "checks": [{"name": "gate", "status": "pass"}],
        },
    )
    assert [c.verdict for c in review.criteria] == ["met", "met"]
