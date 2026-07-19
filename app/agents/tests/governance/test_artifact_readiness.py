from __future__ import annotations

import json

import pytest

from specgate_agents.governance.board.quality_gates import (
    _frozen_gate_rubrics,
    _required_roles_evaluation,
    run_llm_gates_for_artifact,
    run_llm_gates_for_change_request,
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


# --- gate-consumes-Skills: frozen rubric extraction ---


def test_frozen_gate_rubrics_extracts_snapshot_content() -> None:
    artifact = {
        "policy_snapshot_json": json.dumps(
            {
                "snapshot_schema_version": "specgate.policy/v1",
                "approval_policy": "human_required",
                "evidence_policy": "attested_ok",
                "gate_definitions": [
                    {
                        "key": "scope_clear",
                        "version": "v1",
                        "skill_name": "prd-review",
                        "skill_content": "Frozen PRD rubric",
                        "skill_digest": "sha256:frozen",
                    }
                ],
            }
        )
    }
    assert _frozen_gate_rubrics(artifact) == {"scope_clear": "Frozen PRD rubric"}


def test_frozen_gate_rubrics_empty_when_absent_or_malformed() -> None:
    assert _frozen_gate_rubrics({}) == {}
    assert _frozen_gate_rubrics({"policy_snapshot_json": "not json"}) == {}
    assert _frozen_gate_rubrics({"policy_snapshot_json": json.dumps({})}) == {}


class _AsyncNoop:
    async def __call__(self) -> None:
        return None


@pytest.mark.asyncio
async def test_run_llm_gates_requires_workspace_before_registry(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.DocRegistryClient", lambda *_args: pytest.fail("registry called")
    )

    with pytest.raises(ValueError, match="workspace_id is required"):
        await run_llm_gates_for_artifact("art-1")

    with pytest.raises(ValueError, match="workspace_id is required"):
        await run_llm_gates_for_change_request("cr-1")


@pytest.mark.asyncio
async def test_run_llm_gates_for_artifact_uses_snapshot_profile(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, object] = {}

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            assert base_url == "http://registry.test"

        async def aget_artifact(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            assert artifact_id == "art-1"
            return {
                "id": artifact_id,
                "feature_id": "feat-checkout",
                "request_type": "new_feature",
                "policy_snapshot_json": json.dumps(
                    {
                        "snapshot_schema_version": "specgate.policy/v1",
                        "approval_policy": "human_required",
                        "evidence_policy": "attested_ok",
                        "enabled_gates": ["scope_clear", "spec_completeness"],
                        "required_topics": ["outcomes", "acceptance_criteria", "verification"],
                    }
                ),
            }

        async def aload_artifact_bundle_by_role(
            self, artifact_id: str, **_kwargs
        ) -> dict[str, str]:
            assert artifact_id == "art-1"
            return {"spec": "# Spec"}

        async def alist_feature_attachments(
            self, feature_id: str, **_kwargs
        ) -> list[dict[str, str]]:
            assert feature_id == "feat-checkout"
            return [
                {"title": "Bug repro", "audience": "gate", "kind": "link", "url": "https://ex.com"}
            ]

        def refresh_artifact_readiness_runs(
            self, artifact_id: str, evaluations: list[dict[str, object]] | None = None, **_kwargs
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

    result = await run_llm_gates_for_artifact("art-1", workspace_id="ws-a")

    assert result["artifact_id"] == "art-1"
    assert result["evaluations_posted"] == 2
    assert result["readiness_runs"] == [{"gate": "scope_clear", "state": "pass"}]
    assert captured["enabled_gates"] == ["scope_clear", "spec_completeness"]
    assert captured["required_topics"] == ["outcomes", "acceptance_criteria", "verification"]
    assert captured["work_type"] == "new_feature"


@pytest.mark.asyncio
async def test_run_llm_gates_resolves_gate_rubrics(monkeypatch: pytest.MonkeyPatch) -> None:
    """The snapshot's frozen rubric is passed to the judge without a mutable Skill read."""
    captured: dict[str, object] = {}

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            pass

        async def aget_artifact(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            return {
                "id": artifact_id,
                "feature_id": "feat-x",
                "request_type": "new_feature",
                "policy_snapshot_json": json.dumps(
                    {
                        "snapshot_schema_version": "specgate.policy/v1",
                        "approval_policy": "human_required",
                        "evidence_policy": "attested_ok",
                        "enabled_gates": ["scope_clear"],
                        "gate_skills": {"scope_clear": "prd-review"},
                        "gate_definitions": [
                            {
                                "key": "scope_clear",
                                "version": "v1",
                                "skill_name": "prd-review",
                                "skill_content": "FROZEN PRD REVIEW RUBRIC",
                                "skill_digest": "sha256:frozen",
                            }
                        ],
                    }
                ),
            }

        async def aload_artifact_bundle_by_role(
            self, artifact_id: str, **_kwargs
        ) -> dict[str, str]:
            return {"spec": "# Spec"}

        async def alist_feature_attachments(
            self, feature_id: str, **_kwargs
        ) -> list[dict[str, str]]:
            return []

        def refresh_artifact_readiness_runs(self, artifact_id, evaluations=None, **_kwargs):  # noqa: ANN001
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

    await run_llm_gates_for_artifact("art-1", workspace_id="ws-a")

    assert captured["gate_rubrics"] == {"scope_clear": "FROZEN PRD REVIEW RUBRIC"}


@pytest.mark.asyncio
async def test_run_llm_gates_dispatches_to_ide_agent_when_no_model(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, object] = {}

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            pass

        async def aget_artifact(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            return {
                "id": artifact_id,
                "feature_id": "feat",
                "request_type": "new_feature",
                "policy_snapshot_json": json.dumps(
                    {
                        "snapshot_schema_version": "specgate.policy/v1",
                        "approval_policy": "human_required",
                        "evidence_policy": "attested_ok",
                        "enabled_gates": ["scope_clear", "spec_completeness"],
                    }
                ),
            }

        async def adispatch_gate_tasks(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            captured["dispatched_artifact"] = artifact_id
            return {
                "artifact_id": artifact_id,
                "created_task_ids": ["t1", "t2"],
                "skipped_gate_keys": [],
                "pending_task_ids": ["t1", "t2"],
            }

        def refresh_artifact_readiness_runs(
            self, artifact_id: str, evaluations: list[dict[str, object]] | None = None, **_kwargs
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

    result = await run_llm_gates_for_artifact("art-1", workspace_id="ws-a")

    assert result["dispatched_to_ide_agent"]["created_task_ids"] == ["t1", "t2"]
    assert result["dispatched_to_ide_agent"]["pending_task_ids"] == ["t1", "t2"]
    assert captured["dispatched_artifact"] == "art-1"
    assert "evaluate_called" not in captured
    assert result["evaluations_posted"] == 0


@pytest.mark.asyncio
async def test_run_llm_gates_unsupported_snapshot_hides_exception_text(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class FakeClient:
        def __init__(self, base_url: str) -> None:
            pass

        async def aget_artifact(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            return {
                "id": artifact_id,
                "policy_snapshot_json": json.dumps(
                    {"snapshot_schema_version": "internal/debug/schema-v999"}
                ),
            }

    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.DocRegistryClient", FakeClient)
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.doc_registry_base_url",
        lambda: "http://registry.test",
    )

    result = await run_llm_gates_for_artifact("art-unsupported", workspace_id="ws-a")

    assert result == {
        "artifact_id": "art-unsupported",
        "evaluations_posted": 0,
        "readiness_runs": [],
        "compatibility_error": "unsupported_policy_snapshot_version",
    }
    assert "internal/debug/schema-v999" not in json.dumps(result)


@pytest.mark.asyncio
@pytest.mark.parametrize("snapshot", ["{not-json", {"unexpected": "object"}])
async def test_run_llm_gates_corrupt_snapshot_fails_closed_without_refresh(
    monkeypatch: pytest.MonkeyPatch,
    snapshot: object,
) -> None:
    class FakeClient:
        def __init__(self, base_url: str) -> None:
            pass

        async def aget_artifact(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            return {"id": artifact_id, "policy_snapshot_json": snapshot}

        def refresh_artifact_readiness_runs(self, *_args, **_kwargs):  # noqa: ANN001
            pytest.fail("corrupt snapshot must not persist a refreshed readiness run")

    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.DocRegistryClient", FakeClient)
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.doc_registry_base_url",
        lambda: "http://registry.test",
    )

    result = await run_llm_gates_for_artifact("art-corrupt", workspace_id="ws-a")

    assert result == {
        "artifact_id": "art-corrupt",
        "evaluations_posted": 0,
        "readiness_runs": [],
        "compatibility_error": "invalid_policy_snapshot",
    }


@pytest.mark.asyncio
async def test_run_llm_gates_for_change_request_uses_snapshot_profile(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """The CR-level gate path must honor the lead artifact's policy snapshot
    exactly like the artifact-level path: enabled_gates and required_topics
    thread into evaluate_all_gates instead of running every gate."""
    captured: dict[str, object] = {}

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            assert base_url == "http://registry.test"

        def get_change_request(self, change_request_id: str, **_kwargs) -> dict[str, object]:
            assert change_request_id == "cr-1"
            return {
                "id": change_request_id,
                "lead_artifact_id": "art-9",
                "work_type": "bug_fix",
            }

        async def aget_artifact(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            assert artifact_id == "art-9"
            return {
                "id": artifact_id,
                "feature_id": "feat-checkout",
                "policy_snapshot_json": json.dumps(
                    {
                        "snapshot_schema_version": "specgate.policy/v1",
                        "approval_policy": "human_required",
                        "evidence_policy": "attested_ok",
                        "enabled_gates": ["scope_clear"],
                        "required_topics": ["outcomes"],
                    }
                ),
            }

        async def aload_artifact_bundle_by_role(
            self, artifact_id: str, **_kwargs
        ) -> dict[str, str]:
            return {"spec": "# Spec"}

        async def alist_feature_attachments(
            self, feature_id: str, **_kwargs
        ) -> list[dict[str, str]]:
            return []

        async def aget_skills(self, *, workspace_id: str) -> list[dict[str, str]]:
            assert workspace_id == "ws-a"
            return []

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict[str, object]] | None = None,
            **_kwargs,
        ) -> list[dict[str, object]]:
            captured["gate_runs_posted"] = evaluations
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
        captured["enabled_gates"] = list(enabled_gates or [])
        captured["required_topics"] = list(required_topics or [])
        return [GateEvaluation(gate="scope_clear", state="pass", confidence=0.9)]

    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.DocRegistryClient", FakeClient)
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.doc_registry_base_url", lambda: "http://registry.test"
    )
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}._hydrate_model_settings", _AsyncNoop())
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.ensure_llm_env", lambda: True)
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.build_model", lambda: object())
    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.evaluate_all_gates", fake_evaluate_all_gates)

    result = await run_llm_gates_for_change_request("cr-1", workspace_id="ws-a")

    assert result["change_request_id"] == "cr-1"
    assert captured["enabled_gates"] == ["scope_clear"]
    assert captured["required_topics"] == ["outcomes"]


@pytest.mark.asyncio
async def test_change_request_unsupported_snapshot_fails_closed_without_refresh(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class FakeClient:
        def __init__(self, base_url: str) -> None:
            pass

        def get_change_request(self, change_request_id: str, **_kwargs) -> dict[str, object]:
            return {
                "id": change_request_id,
                "lead_artifact_id": "art-unsupported",
                "work_type": "bug_fix",
            }

        async def aget_artifact(self, artifact_id: str, **_kwargs) -> dict[str, object]:
            return {
                "id": artifact_id,
                "policy_snapshot_json": json.dumps(
                    {"snapshot_schema_version": "specgate.policy/v999"}
                ),
            }

        def refresh_change_request_gate_runs(self, *_args, **_kwargs):  # noqa: ANN001
            pytest.fail("unsupported snapshot must not persist refreshed gate runs")

    monkeypatch.setattr(f"{QUALITY_GATES_MODULE}.DocRegistryClient", FakeClient)
    monkeypatch.setattr(
        f"{QUALITY_GATES_MODULE}.doc_registry_base_url",
        lambda: "http://registry.test",
    )

    result = await run_llm_gates_for_change_request("cr-unsupported", workspace_id="ws-a")

    assert result == {
        "change_request_id": "cr-unsupported",
        "lead_artifact_id": "art-unsupported",
        "evaluations_posted": 0,
        "gate_runs": [],
        "governance_level": "",
        "compatibility_error": "unsupported_policy_snapshot_version",
    }
