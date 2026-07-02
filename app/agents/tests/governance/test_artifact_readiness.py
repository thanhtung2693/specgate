from __future__ import annotations

import json

import pytest

from specgate_agents.governance.board.quality_gates import (
    _profile_gate_skills,
    _profile_readiness_config,
    _required_roles_evaluation,
    resolve_gate_rubrics,
    run_llm_gates_for_artifact,
)
from specgate_agents.governance.quality_gates.judge import GateEvaluation

QUALITY_GATES_MODULE = "specgate_agents.governance.board.quality_gates"


def test_required_roles_evaluation_warns_on_missing() -> None:
    ev = _required_roles_evaluation(["spec", "design", "verification"], {"spec"})
    assert ev.gate == "required_roles_present"
    assert ev.state == "warn"
    detail = json.loads(ev.evidence)
    assert detail["missing_roles"] == ["design", "verification"]


def test_required_roles_evaluation_passes_when_all_present() -> None:
    ev = _required_roles_evaluation(["spec", "design"], {"spec", "design", "plan"})
    assert ev.state == "pass"
    assert json.loads(ev.evidence)["missing_roles"] == []


def test_profile_readiness_config_extracts_roles() -> None:
    artifact = {
        "gates_profile_snapshot_json": json.dumps(
            {
                "enabled_gates": ["spec_completeness"],
                "required_topics": ["outcomes"],
                "required_roles": ["spec", "design"],
            }
        )
    }
    gates, topics, roles = _profile_readiness_config(artifact)
    assert gates == ["spec_completeness"]
    assert topics == ["outcomes"]
    assert roles == ["spec", "design"]


# --- gate-consumes-Skills: gate_skills extraction + rubric resolution ---


def test_profile_gate_skills_extracts_map() -> None:
    artifact = {
        "gates_profile_snapshot_json": json.dumps(
            {"gate_skills": {"scope_clear": "prd-review", "spec_completeness": "spec-review"}}
        )
    }
    assert _profile_gate_skills(artifact) == {
        "scope_clear": "prd-review",
        "spec_completeness": "spec-review",
    }


def test_profile_gate_skills_empty_when_absent_or_malformed() -> None:
    assert _profile_gate_skills({}) == {}
    assert _profile_gate_skills({"gates_profile_snapshot_json": "not json"}) == {}
    assert _profile_gate_skills({"gates_profile_snapshot_json": json.dumps({})}) == {}


def test_resolve_gate_rubrics_maps_gate_to_skill_prompt() -> None:
    skills = [
        {"name": "prd-review", "prompt": "PRD RUBRIC"},
        {"name": "spec-review", "prompt": "SPEC RUBRIC"},
    ]
    out = resolve_gate_rubrics(
        {"scope_clear": "prd-review", "spec_completeness": "spec-review"}, skills
    )
    assert out == {"scope_clear": "PRD RUBRIC", "spec_completeness": "SPEC RUBRIC"}


def test_resolve_gate_rubrics_skips_missing_or_blank_skill() -> None:
    skills = [{"name": "prd-review", "prompt": "   "}]  # blank prompt
    out = resolve_gate_rubrics({"scope_clear": "prd-review", "x": "no-such-skill"}, skills)
    assert out == {}


class _AsyncNoop:
    async def __call__(self) -> None:
        return None


@pytest.mark.asyncio
async def test_run_llm_gates_for_artifact_uses_snapshot_profile(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, object] = {}

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            assert base_url == "http://registry.test"

        async def aget_artifact(self, artifact_id: str) -> dict[str, object]:
            assert artifact_id == "art-1"
            return {
                "id": artifact_id,
                "feature_id": "feat-checkout",
                "request_type": "new_feature",
                "gates_profile_snapshot_json": json.dumps(
                    {
                        "enabled_gates": ["scope_clear", "spec_completeness"],
                        "required_topics": ["outcomes", "acceptance_criteria", "verification"],
                    }
                ),
            }

        async def aload_artifact_bundle_by_role(self, artifact_id: str) -> dict[str, str]:
            assert artifact_id == "art-1"
            return {"spec": "# Spec"}

        async def alist_feature_attachments(self, feature_id: str) -> list[dict[str, str]]:
            assert feature_id == "feat-checkout"
            return [
                {"title": "Bug repro", "audience": "gate", "kind": "link", "url": "https://ex.com"}
            ]

        def refresh_artifact_readiness_runs(
            self, artifact_id: str, evaluations: list[dict[str, object]] | None = None
        ) -> list[dict[str, object]]:
            captured["artifact_id"] = artifact_id
            captured["evaluations"] = evaluations
            return [{"gate": "scope_clear", "state": "pass"}]

    async def fake_evaluate_all_gates(
        artifact_bundle: dict[str, str],
        *,
        model,
        judge_model: str = "governance-gate-judge",
        work_type: str = "",
        config=None,
        attachments=None,
        enabled_gates=None,
        required_topics=None,
        gate_rubrics=None,
    ) -> list[GateEvaluation]:
        captured["bundle"] = artifact_bundle
        captured["work_type"] = work_type
        captured["attachments"] = attachments
        captured["enabled_gates"] = list(enabled_gates or [])
        captured["required_topics"] = list(required_topics or [])
        captured["gate_rubrics"] = dict(gate_rubrics or {})
        return [
            GateEvaluation(gate="scope_clear", state="pass", confidence=0.9),
            GateEvaluation(gate="spec_completeness", state="warn", confidence=0.9),
        ]

    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.DocRegistryClient", FakeClient)
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.doc_registry_base_url",
        lambda: "http://registry.test",
    )
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}._hydrate_model_settings",
        _AsyncNoop(),
    )
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.ensure_llm_env", lambda: True)
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.build_model", lambda: object())
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.evaluate_all_gates",
        fake_evaluate_all_gates,
    )

    result = await run_llm_gates_for_artifact("art-1")

    assert result["artifact_id"] == "art-1"
    assert result["evaluations_posted"] == 2
    assert result["readiness_runs"] == [{"gate": "scope_clear", "state": "pass"}]
    assert captured["enabled_gates"] == ["scope_clear", "spec_completeness"]
    assert captured["required_topics"] == ["outcomes", "acceptance_criteria", "verification"]
    assert captured["work_type"] == "new_feature"


@pytest.mark.asyncio
async def test_run_llm_gates_resolves_gate_rubrics(monkeypatch: pytest.MonkeyPatch) -> None:
    """The snapshot's gate_skills bindings are resolved to rubric prompts and passed
    to evaluate_all_gates (gate-consumes-Skills end-to-end at the board layer)."""
    captured: dict[str, object] = {}

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            pass

        async def aget_artifact(self, artifact_id: str) -> dict[str, object]:
            return {
                "id": artifact_id,
                "feature_id": "feat-x",
                "request_type": "new_feature",
                "gates_profile_snapshot_json": json.dumps(
                    {
                        "enabled_gates": ["scope_clear"],
                        "gate_skills": {"scope_clear": "prd-review"},
                    }
                ),
            }

        async def aload_artifact_bundle_by_role(self, artifact_id: str) -> dict[str, str]:
            return {"spec": "# Spec"}

        async def alist_feature_attachments(self, feature_id: str) -> list[dict[str, str]]:
            return []

        async def aget_skills(self) -> list[dict[str, str]]:
            return [{"name": "prd-review", "prompt": "PRD REVIEW RUBRIC"}]

        def refresh_artifact_readiness_runs(self, artifact_id, evaluations=None):  # noqa: ANN001
            return []

    async def fake_evaluate_all_gates(artifact_bundle, **kwargs):  # noqa: ANN001
        captured["gate_rubrics"] = dict(kwargs.get("gate_rubrics") or {})
        return [GateEvaluation(gate="scope_clear", state="pass", confidence=0.9)]

    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.DocRegistryClient", FakeClient)
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.doc_registry_base_url", lambda: "http://r.test")
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}._hydrate_model_settings", _AsyncNoop())
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.ensure_llm_env", lambda: True)
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.build_model", lambda: object())
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.evaluate_all_gates", fake_evaluate_all_gates)

    await run_llm_gates_for_artifact("art-1")

    assert captured["gate_rubrics"] == {"scope_clear": "PRD REVIEW RUBRIC"}


@pytest.mark.asyncio
async def test_run_llm_gates_dispatches_to_ide_agent_when_no_model(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, object] = {}

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            pass

        async def aget_artifact(self, artifact_id: str) -> dict[str, object]:
            return {
                "id": artifact_id,
                "feature_id": "feat",
                "request_type": "new_feature",
                "gates_profile_snapshot_json": json.dumps(
                    {"enabled_gates": ["scope_clear", "spec_completeness"]}
                ),
            }

        async def adispatch_gate_tasks(self, artifact_id: str) -> dict[str, object]:
            captured["dispatched_artifact"] = artifact_id
            return {
                "artifact_id": artifact_id,
                "created_task_ids": ["t1", "t2"],
                "skipped_gate_keys": [],
            }

        def refresh_artifact_readiness_runs(
            self, artifact_id: str, evaluations: list[dict[str, object]] | None = None
        ) -> list[dict[str, object]]:
            captured["evaluations"] = evaluations
            return []

    async def fail_evaluate(*args: object, **kwargs: object) -> list[GateEvaluation]:
        captured["evaluate_called"] = True
        return []

    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.DocRegistryClient", FakeClient)
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.doc_registry_base_url",
        lambda: "http://registry.test",
    )
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}._hydrate_model_settings", _AsyncNoop())
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.ensure_llm_env", lambda: False)
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.evaluate_all_gates", fail_evaluate)

    result = await run_llm_gates_for_artifact("art-1")

    assert result["dispatched_to_ide_agent"]["created_task_ids"] == ["t1", "t2"]
    assert captured["dispatched_artifact"] == "art-1"
    assert "evaluate_called" not in captured
    assert result["evaluations_posted"] == 0
