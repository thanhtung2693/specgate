from __future__ import annotations

from fastapi.testclient import TestClient

from specgate_agents.governance.webapp import app


def test_artifact_readiness_route_calls_runner(monkeypatch) -> None:
    captured: dict[str, str] = {}

    async def fake_run(artifact_id: str) -> dict[str, object]:
        captured["artifact_id"] = artifact_id
        return {"artifact_id": artifact_id, "evaluations_posted": 1, "readiness_runs": []}

    monkeypatch.setattr("specgate_agents.governance.webapp.run_llm_gates_for_artifact", fake_run)

    client = TestClient(app)
    response = client.post("/artifacts/art-7/run-readiness")

    assert response.status_code == 200
    assert response.json()["artifact_id"] == "art-7"
    assert captured["artifact_id"] == "art-7"

