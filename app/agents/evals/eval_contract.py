from __future__ import annotations

import json
from pathlib import Path
from typing import Literal

from langchain_core.language_models.chat_models import BaseChatModel
from pydantic import BaseModel, Field

Verdict = Literal["pass", "warn", "fail", "needs_human_review", "not_applicable"]


class EvalInput(BaseModel):
    artifact_md: str
    knowledge_snippets: list[str] = Field(default_factory=list)
    feedback_events: list[str] = Field(default_factory=list)


class EvalExpectation(BaseModel):
    verdict: Verdict
    confidence_min: float = Field(ge=0.0, le=1.0)


class EvalCase(BaseModel):
    case_id: str
    suite: str
    input: EvalInput
    expected: EvalExpectation


class EvalResult(BaseModel):
    case_id: str
    suite: str
    actual: dict
    expected: dict
    score: float
    passed: bool
    diagnostics: list[str] = Field(default_factory=list)


class FakeJudge:
    def evaluate(self, case: EvalCase) -> tuple[Verdict, float, list[str]]:
        if case.suite == "quality_gate":
            body = case.input.artifact_md.lower()
            has_rollback = "rollback" in body
            has_metric = "metric" in body or "kpi" in body
            confidence = 0.9 if has_rollback and has_metric else 0.4
            verdict: Verdict = "pass" if has_rollback and has_metric else "warn"
            diagnostics = []
            if not has_rollback:
                diagnostics.append("missing rollback plan")
            if not has_metric:
                diagnostics.append("missing measurable metric")
            return verdict, confidence, diagnostics
        if case.suite == "ac_satisfaction":
            body = case.input.artifact_md.lower()
            has_criteria = "acceptance criteria" in body or "ac:" in body
            has_evidence = any("test" in item.lower() for item in case.input.feedback_events)
            verdict = "pass" if has_criteria and has_evidence else "warn"
            confidence = 0.85 if verdict == "pass" else 0.45
            diagnostics = []
            if not has_criteria:
                diagnostics.append("missing acceptance criteria trace")
            if not has_evidence:
                diagnostics.append("missing implementation evidence")
            return verdict, confidence, diagnostics
        return "needs_human_review", 0.2, [f"unsupported suite: {case.suite}"]


class LiveJudgeDecision(BaseModel):
    verdict: Verdict
    confidence: float = Field(ge=0.0, le=1.0)
    diagnostics: list[str] = Field(default_factory=list)


class LiveJudge:
    def __init__(self, model: BaseChatModel) -> None:
        self._model = model

    async def evaluate(self, case: EvalCase) -> tuple[Verdict, float, list[str]]:
        prompt = (
            "You are evaluating an SDLC artifact quality contract.\n"
            f"Suite: {case.suite}\n"
            "Return only JSON object with fields: verdict, confidence, diagnostics.\n"
            f"Artifact markdown:\n{case.input.artifact_md}\n"
            f"Knowledge snippets: {case.input.knowledge_snippets}\n"
            f"Feedback events: {case.input.feedback_events}\n"
        )
        decision_model = self._model.with_structured_output(LiveJudgeDecision)
        decision = await decision_model.ainvoke(prompt)
        return decision.verdict, decision.confidence, decision.diagnostics


def load_eval_cases(path: Path) -> list[EvalCase]:
    rows: list[EvalCase] = []
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        rows.append(EvalCase.model_validate(json.loads(line)))
    return rows


def run_fake_judge_suite(cases: list[EvalCase], judge: FakeJudge) -> list[EvalResult]:
    out: list[EvalResult] = []
    for case in cases:
        verdict, confidence, diagnostics = judge.evaluate(case)
        passed = verdict == case.expected.verdict and confidence >= case.expected.confidence_min
        score = 1.0 if passed else 0.0
        out.append(
            EvalResult(
                case_id=case.case_id,
                suite=case.suite,
                actual={"verdict": verdict, "confidence": confidence},
                expected=case.expected.model_dump(),
                score=score,
                passed=passed,
                diagnostics=diagnostics,
            )
        )
    return out
