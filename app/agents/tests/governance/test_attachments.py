"""Feature reference-attachment helpers — audience filter + markdown render."""

from __future__ import annotations

from specgate_agents.governance.attachments import (
    filter_by_audience,
    render_attachments_section,
)


def _att(**kw):
    base = {
        "kind": "link",
        "url": "",
        "governance_file_id": "",
        "title": "",
        "note": "",
        "audience": "gate",
    }
    base.update(kw)
    return base


def test_filter_by_audience_includes_both() -> None:
    items = [
        _att(audience="gate", title="g"),
        _att(audience="coding_agent", title="c"),
        _att(audience="both", title="b"),
    ]
    coding = filter_by_audience(items, ("coding_agent", "both"))
    assert {a["title"] for a in coding} == {"c", "b"}
    gate = filter_by_audience(items, ("gate", "both"))
    assert {a["title"] for a in gate} == {"g", "b"}


def test_render_empty_when_no_match() -> None:
    items = [_att(audience="gate")]
    assert render_attachments_section(items, audiences=("coding_agent", "both")) == []


def test_render_link_and_file_via_content_proxy() -> None:
    items = [
        _att(audience="both", kind="link", url="https://figma.com/x", title="Design"),
        _att(audience="coding_agent", kind="image", governance_file_id="pf-7", title="Bug shot"),
    ]
    lines = render_attachments_section(
        items,
        audiences=("coding_agent", "both"),
        base_url="http://doc-registry:8080/",
    )
    blob = "\n".join(lines)
    assert "## Reference Attachments" in blob
    assert "https://figma.com/x" in blob
    # File/image rows resolve to the content proxy, never an S3 URL.
    assert "http://doc-registry:8080/governance/files/pf-7/content" in blob


def test_gate_threads_gate_audience_attachments_into_edge_and_scope(monkeypatch) -> None:
    import asyncio

    from specgate_agents.governance.quality_gates import judge

    captured: dict[str, str] = {}

    async def _capture(key):
        async def _fn(md, **kw):
            captured[key] = md
            return judge.GateEvaluation(gate="x", state="pass", confidence=1.0, evidence="")

        return _fn

    # Replace the LLM evaluators with capturing stubs (keyed by gate) so the
    # gather runs without a real model.
    for name, key in [
        ("evaluate_rollback_plan", "rollback"),
        ("evaluate_acceptance_criteria_edge_cases", "ac"),
        ("evaluate_acceptance_criteria_verifiable", "ac_verifiable"),
        ("evaluate_success_metric_measurable", "metric"),
        ("evaluate_scope_clear", "scope"),
        ("evaluate_implementation_plan_traceable", "traceable"),
    ]:
        monkeypatch.setattr(judge, name, asyncio.run(_capture(key)))

    # The completeness judge is imported function-level inside evaluate_all_gates,
    # so patch it on its own module (it always calls the model otherwise).
    from specgate_agents.governance.quality_gates import completeness

    monkeypatch.setattr(
        completeness, "evaluate_spec_completeness", asyncio.run(_capture("completeness"))
    )

    bundle = {
        "spec": "S",
        "plan": "P",
        "verification": "Q",
        "reference": "R",
    }
    atts = [
        _att(audience="gate", kind="link", url="https://ex.com/repro", title="Repro"),
        _att(audience="coding_agent", kind="link", url="https://ex.com/skip", title="Skip"),
    ]
    asyncio.run(judge.evaluate_all_gates(bundle, model=object(), attachments=atts))

    assert "https://ex.com/repro" in captured["ac"]
    assert "https://ex.com/repro" in captured["scope"]
    # coding_agent-only attachment never reaches the gate.
    assert "https://ex.com/skip" not in captured["ac"]
    # Gates that don't take attachments stay clean.
    assert "https://ex.com/repro" not in captured["rollback"]
