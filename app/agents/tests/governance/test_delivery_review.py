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
async def test_review_delivery_hydrates_model_omitted_criterion_text() -> None:
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[
                CriterionReview(criterion_id="ac-1", verdict="met", text=""),
                CriterionReview(criterion_id="ac-2", verdict="met", text=""),
            ],
            confidence=0.9,
        )
    )
    review = await review_delivery(
        acceptance_criteria=AC,
        completed_payload=_payload(checks=[{"name": "tests", "status": "pass"}]),
        model=model,
    )
    assert [criterion.text for criterion in review.criteria] == [
        "Import disabled when disconnected",
        "Show CTA",
    ]


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
        ALL_MET,
        GREEN_CHECKS,
        confidence=0.9,
        evidence_policy="corroborated_required",
        has_corroborated=False,
    )
    assert state == "needs_human_review"


def test_resolve_overall_corroborated_required_with_corroborated_allows_pass() -> None:
    """corroborated_required + corroborated evidence present → pass is allowed."""
    state = _resolve_overall(
        ALL_MET,
        GREEN_CHECKS,
        confidence=0.9,
        evidence_policy="corroborated_required",
        has_corroborated=True,
    )
    assert state == "pass"


def test_resolve_overall_corroborated_required_all_bound_allows_pass() -> None:
    """Canonical deterministic check bindings satisfy corroborated_required."""
    state = _resolve_overall(
        [
            CriterionReview(
                criterion_id="ac-1",
                verdict="met",
                verification_binding="tests",
                trust_tier="deterministic",
            ),
            CriterionReview(
                criterion_id="ac-2",
                verdict="met",
                verification_binding="integration",
                trust_tier="deterministic",
            ),
        ],
        GREEN_CHECKS,
        confidence=0.9,
        evidence_policy="corroborated_required",
        has_corroborated=False,
    )
    assert state == "pass"


def test_resolve_overall_attested_ok_no_clamp() -> None:
    """attested_ok (default) → no clamp regardless of has_corroborated."""
    state = _resolve_overall(
        ALL_MET,
        GREEN_CHECKS,
        confidence=0.9,
        evidence_policy="attested_ok",
        has_corroborated=False,
    )
    assert state == "pass"


def test_resolve_overall_corroborated_required_fail_unchanged() -> None:
    """Clamp only affects pass; fail stays fail even with no corroborated evidence."""
    from specgate_agents.governance.quality_gates.delivery_review import CriterionReview

    state = _resolve_overall(
        [CriterionReview(criterion_id="ac-1", verdict="unmet")],
        [],
        confidence=0.9,
        evidence_policy="corroborated_required",
        has_corroborated=False,
    )
    assert state == "fail"


def test_resolve_overall_corroborated_required_already_needs_human_unchanged() -> None:
    """Clamp is a no-op when base is already needs_human_review."""
    state = _resolve_overall(
        [CriterionReview(criterion_id="ac-1", verdict="unclear")],
        [],
        confidence=0.9,
        evidence_policy="corroborated_required",
        has_corroborated=False,
    )
    assert state == "needs_human_review"


@pytest.mark.asyncio
async def test_review_delivery_threads_evidence_policy_and_has_corroborated() -> None:
    """review_delivery accepts evidence_policy + has_corroborated and threads them to clamp."""
    model = _FakeModel(DeliveryReviewJudgment(criteria=list(ALL_MET), confidence=0.9))
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


def test_derive_rejects_status_prefixed_claim_text() -> None:
    r = derive_review_from_claims(
        acceptance_criteria=_ACS,
        completed_payload={
            "criteria": [
                {"criterion_id": "ac_1", "claim": "satisfied: implemented in app/ui"},
                {"criterion_id": "ac_2", "claim": "satisfied - covered by App.test.tsx"},
            ],
            "checks": [{"name": "tests", "status": "pass"}],
        },
    )
    assert r.state == "needs_human_review"
    assert [c.verdict for c in r.criteria] == ["unclear", "unclear"]


def test_derive_rejects_uppercase_claim_alias() -> None:
    review = derive_review_from_claims(
        acceptance_criteria=[_ACS[0]],
        completed_payload={"criteria": [{"criterion_id": "ac_1", "claim": "SATISFIED"}]},
    )
    assert review.state == "needs_human_review"
    assert review.criteria[0].verdict == "unclear"


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


def test_derive_grounded_evidence_records_grounded_tier() -> None:
    review = derive_review_from_claims(
        acceptance_criteria=[{"id": "ac_1", "text": "Handler exists"}],
        completed_payload={
            "criteria": [
                {
                    "criterion_id": "ac_1",
                    "claim": "satisfied",
                    "evidence": {
                        "kind": "file",
                        "path": "handler.go",
                        "grounding": {
                            "status": "grounded",
                            "excerpt": "func Handler() {}",
                            "digest": "sha256:abc",
                        },
                    },
                }
            ],
            "checks": [{"name": "tests", "status": "pass"}],
        },
    )
    assert review.criteria[0].trust_tier == "grounded"
    assert review.state == "pass"


def test_derive_review_requires_canonical_criterion_ids() -> None:
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
    assert [c.verdict for c in review.criteria] == ["unclear", "unclear"]


# ── deterministic verification_binding ───────────────────────────────────────
# These prove the per-criterion binding resolves with NO LLM. Using the delivery
# review model to verify changes to the delivery review would be circular.

from specgate_agents.governance.quality_gates.delivery_review import (  # noqa: E402
    _resolve_bound_criterion,
)

_BOUND_AC = {"id": "ac-0", "text": "Migration adds the column", "verification_binding": "migrate"}


def test_resolve_bound_criterion_pass_is_met_confidence_and_reason() -> None:
    review = _resolve_bound_criterion(_BOUND_AC, [{"name": "migrate", "status": "pass"}])
    assert review.verdict == "met"
    assert review.verification_binding == "migrate"
    assert "migrate" in review.why  # reason names the bound check


def test_resolve_bound_criterion_fail_is_unmet() -> None:
    review = _resolve_bound_criterion(_BOUND_AC, [{"name": "migrate", "status": "fail"}])
    assert review.verdict == "unmet"
    assert "migrate" in review.why


def test_resolve_bound_criterion_missing_check_is_unclear() -> None:
    review = _resolve_bound_criterion(_BOUND_AC, [{"name": "tests", "status": "pass"}])
    assert review.verdict == "unclear"  # named check absent → never silently met


def test_resolve_bound_criterion_skipped_check_is_unclear() -> None:
    review = _resolve_bound_criterion(_BOUND_AC, [{"name": "migrate", "status": "skipped"}])
    assert review.verdict == "unclear"


def test_resolve_bound_criterion_rejects_uppercase_check_alias() -> None:
    review = _resolve_bound_criterion(_BOUND_AC, [{"name": "migrate", "status": "PASS"}])
    assert review.verdict == "unclear"


def test_derive_bound_pass_is_met_with_no_model() -> None:
    """Board-level no-LLM path: a bound criterion's verdict comes from the check,
    not the agent's claim (here the agent under-claims 'partial' but the check passed)."""
    review = derive_review_from_claims(
        acceptance_criteria=[_BOUND_AC],
        completed_payload={
            "criteria": [{"criterion_id": "ac-0", "claim": "partial"}],
            "checks": [{"name": "migrate", "status": "pass"}],
        },
    )
    assert review.judge_model == "deterministic_checks"
    assert review.hint == "Verdict derived from locally reproduced deterministic checks."
    assert [c.verdict for c in review.criteria] == ["met"]
    assert review.state == "pass"
    assert review.confidence == 1.0  # bound verdict is explicit, not downgraded


def test_derive_bound_fail_overrides_optimistic_claim() -> None:
    """The agent claims satisfied but the bound check failed → unmet → fail."""
    review = derive_review_from_claims(
        acceptance_criteria=[_BOUND_AC],
        completed_payload={
            "criteria": [{"criterion_id": "ac-0", "claim": "satisfied"}],
            "checks": [{"name": "migrate", "status": "fail"}],
        },
    )
    assert [c.verdict for c in review.criteria] == ["unmet"]
    assert review.state == "fail"


def test_derive_bound_missing_check_needs_human_review() -> None:
    review = derive_review_from_claims(
        acceptance_criteria=[_BOUND_AC],
        completed_payload={
            "criteria": [{"criterion_id": "ac-0", "claim": "satisfied"}],
            "checks": [{"name": "tests", "status": "pass"}],
        },
    )
    assert [c.verdict for c in review.criteria] == ["unclear"]
    assert review.state == "needs_human_review"


@pytest.mark.asyncio
async def test_review_delivery_bound_criterion_bypasses_model() -> None:
    """An all-bound review resolves from checks with NO model call (confidence 1.0)."""

    class _ExplodingModel:
        def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
            raise AssertionError("model must not be called for a fully-bound review")

    review = await review_delivery(
        acceptance_criteria=[_BOUND_AC],
        completed_payload=_payload(checks=[{"name": "migrate", "status": "pass"}]),
        model=_ExplodingModel(),
    )
    assert [c.verdict for c in review.criteria] == ["met"]
    assert review.confidence == 1.0
    assert review.state == "pass"
    # All-bound review has no model summary; a deterministic hint fills the gap.
    assert review.hint == "1/1 criteria verified deterministically from checks"


@pytest.mark.asyncio
async def test_review_delivery_mixed_judges_only_unbound() -> None:
    """A bound criterion is resolved from checks; the model judges only the unbound one."""
    captured: list[str] = []

    class _CapStructured:
        async def ainvoke(self, messages, config=None):  # noqa: ANN001, ANN201
            captured.extend(str(getattr(m, "content", m)) for m in messages)
            return DeliveryReviewJudgment(
                criteria=[CriterionReview(criterion_id="ac-1", verdict="met")],
                confidence=0.9,
            )

    class _CapModel:
        def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
            return _CapStructured()

    review = await review_delivery(
        acceptance_criteria=[
            {"id": "ac-0", "text": "Migration adds column", "verification_binding": "migrate"},
            {"id": "ac-1", "text": "CTA shown"},
        ],
        completed_payload=_payload(checks=[{"name": "migrate", "status": "pass"}]),
        model=_CapModel(),
    )
    # criteria returned in acceptance-criteria order, bound first
    assert [(c.criterion_id, c.verdict) for c in review.criteria] == [
        ("ac-0", "met"),
        ("ac-1", "met"),
    ]
    joined = "\n".join(captured)
    assert "ac-1" in joined  # unbound criterion is judged
    assert "ac-0" not in joined  # bound criterion never shown to the model
    assert review.state == "pass"


@pytest.mark.asyncio
async def test_review_delivery_ignores_model_authored_binding_for_corroboration() -> None:
    """Only canonical AC bindings are authoritative; model-emitted bindings stay advisory."""

    class _FakeBindingModel:
        def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
            return _FakeStructured(
                DeliveryReviewJudgment(
                    criteria=[
                        CriterionReview(
                            criterion_id="ac-1",
                            verdict="met",
                            verification_binding="tests",
                        )
                    ],
                    confidence=0.9,
                )
            )

    review = await review_delivery(
        acceptance_criteria=[{"id": "ac-1", "text": "CTA shown"}],
        completed_payload=_payload(checks=[{"name": "tests", "status": "pass"}]),
        model=_FakeBindingModel(),
        evidence_policy="corroborated_required",
    )
    assert review.criteria[0].verification_binding == ""
    assert review.state == "needs_human_review"


@pytest.mark.asyncio
async def test_review_delivery_records_grounded_tier_for_grounded_evidence() -> None:
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[CriterionReview(criterion_id="ac-1", verdict="met")],
            confidence=0.9,
        )
    )
    review = await review_delivery(
        acceptance_criteria=[{"id": "ac-1", "text": "Handler exists"}],
        completed_payload=_payload(
            checks=[{"name": "tests", "status": "pass"}],
            criteria=[
                {
                    "criterion_id": "ac-1",
                    "claim": "satisfied",
                    "evidence": {
                        "kind": "file",
                        "path": "handler.go",
                        "grounding": {
                            "status": "grounded",
                            "excerpt": "func Handler() {}",
                            "digest": "sha256:abc",
                        },
                    },
                }
            ],
        ),
        model=model,
    )
    assert review.criteria[0].trust_tier == "grounded"
    assert review.state == "pass"


@pytest.mark.asyncio
async def test_review_delivery_no_binding_path_unchanged() -> None:
    """With no verification_binding, review_delivery judges every criterion via the
    model exactly as before (all criteria shown; model confidence used)."""
    captured: list[str] = []

    class _CapStructured:
        async def ainvoke(self, messages, config=None):  # noqa: ANN001, ANN201
            captured.extend(str(getattr(m, "content", m)) for m in messages)
            return DeliveryReviewJudgment(criteria=list(ALL_MET), confidence=0.42)

    class _CapModel:
        def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
            return _CapStructured()

    review = await review_delivery(
        acceptance_criteria=AC,
        completed_payload=_payload(checks=list(GREEN_CHECKS)),
        model=_CapModel(),
    )
    joined = "\n".join(captured)
    assert "ac-1" in joined and "ac-2" in joined  # every criterion shown to the model
    assert review.confidence == 0.42  # model confidence flows through unchanged
    assert [c.criterion_id for c in review.criteria] == ["ac-1", "ac-2"]


@pytest.mark.asyncio
async def test_review_delivery_preserves_canonical_id_when_model_returns_foreign_id() -> None:
    canonical_id = "eb990cec-fc03-4c9e-90b9-e5338e66abc5"
    model = _FakeModel(
        DeliveryReviewJudgment(
            criteria=[CriterionReview(criterion_id="ac-0", verdict="met")],
            confidence=0.9,
        )
    )

    review = await review_delivery(
        acceptance_criteria=[{"id": canonical_id, "text": "Canonical criterion"}],
        completed_payload=_payload(),
        model=model,
    )

    assert [(c.criterion_id, c.verdict) for c in review.criteria] == [(canonical_id, "unclear")]
    assert review.state == "needs_human_review"


@pytest.mark.asyncio
async def test_review_prompt_requires_evidence_for_every_compound_clause() -> None:
    captured: list[str] = []

    class _CaptureStructured:
        async def ainvoke(self, messages, config=None):  # noqa: ANN001, ANN201
            captured.extend(str(getattr(message, "content", message)) for message in messages)
            return DeliveryReviewJudgment(
                criteria=[CriterionReview(criterion_id="ac-1", verdict="unclear")],
                confidence=0.9,
            )

    class _CaptureModel:
        def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
            return _CaptureStructured()

    await review_delivery(
        acceptance_criteria=[{"id": "ac-1", "text": "Tests pass and mobile smoke passes"}],
        completed_payload=_payload(
            criteria=[
                {
                    "criterion_id": "ac-1",
                    "claim": "satisfied",
                    "evidence": {"kind": "file", "path": "tests.py"},
                }
            ]
        ),
        model=_CaptureModel(),
    )

    prompt = "\n".join(captured)
    assert "every independently verifiable clause" in prompt
    assert "generic file or test citation" in prompt


def test_delivery_review_module_has_no_knowledge_search_dependency() -> None:
    import inspect

    from specgate_agents.governance.quality_gates import delivery_review

    source = inspect.getsource(delivery_review)
    assert "search_knowledge" not in source
    assert "/governance/context/search" not in source
