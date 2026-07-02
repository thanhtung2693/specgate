from __future__ import annotations

from pathlib import Path

from evals.eval_contract import (
    EvalCase,
    EvalExpectation,
    EvalInput,
    FakeJudge,
    load_eval_cases,
    run_fake_judge_suite,
)


def test_load_eval_cases_parses_required_fields(tmp_path: Path) -> None:
    path = tmp_path / "suite.jsonl"
    path.write_text(
        "\n".join(
            [
                '{"case_id":"c1","suite":"quality_gate","input":{"artifact_md":"A","knowledge_snippets":["K1"],"feedback_events":["F1"]},"expected":{"verdict":"pass","confidence_min":0.8}}',
                '{"case_id":"c2","suite":"ac_satisfaction","input":{"artifact_md":"B","knowledge_snippets":[],"feedback_events":[]},"expected":{"verdict":"warn","confidence_min":0.3}}',
            ]
        ),
        encoding="utf-8",
    )
    cases = load_eval_cases(path)
    assert len(cases) == 2
    assert cases[0].case_id == "c1"
    assert cases[1].expected.verdict == "warn"


def test_fake_judge_runner_emits_contract_result_shape() -> None:
    cases = [
        EvalCase(
            case_id="case-pass",
            suite="quality_gate",
            input=EvalInput(
                artifact_md="spec has rollback plan and measurable metrics",
                knowledge_snippets=["knowledge one"],
                feedback_events=["merged"],
            ),
            expected=EvalExpectation(verdict="pass", confidence_min=0.5),
        ),
        EvalCase(
            case_id="case-warn",
            suite="quality_gate",
            input=EvalInput(
                artifact_md="spec missing rollback",
                knowledge_snippets=[],
                feedback_events=[],
            ),
            expected=EvalExpectation(verdict="warn", confidence_min=0.2),
        ),
    ]
    results = run_fake_judge_suite(cases, FakeJudge())
    assert len(results) == 2
    for row in results:
        payload = row.model_dump()
        assert "case_id" in payload
        assert "suite" in payload
        assert "actual" in payload
        assert "expected" in payload
        assert "score" in payload
        assert "passed" in payload
        assert "diagnostics" in payload
