from __future__ import annotations

import httpx

from specgate_agents.governance.webapp import app


def asgi_client() -> httpx.AsyncClient:
    return httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://test")


async def test_artifact_readiness_route_calls_runner(monkeypatch) -> None:
    captured: dict[str, str] = {}

    async def fake_run(artifact_id: str, *, workspace_id: str) -> dict[str, object]:
        captured["artifact_id"] = artifact_id
        captured["workspace_id"] = workspace_id
        return {"artifact_id": artifact_id, "evaluations_posted": 1, "readiness_runs": []}

    monkeypatch.setattr("specgate_agents.governance.webapp.run_llm_gates_for_artifact", fake_run)

    async with asgi_client() as client:
        response = await client.post("/artifacts/art-7/run-readiness?workspace_id=ws-a")

    assert response.status_code == 200
    assert response.json()["artifact_id"] == "art-7"
    assert captured["artifact_id"] == "art-7"
    assert captured["workspace_id"] == "ws-a"


async def test_artifact_readiness_route_hides_exception_detail(monkeypatch) -> None:
    async def fake_run(_artifact_id: str, *, workspace_id: str) -> dict[str, object]:
        raise RuntimeError("secret provider token leaked in stack")

    monkeypatch.setattr("specgate_agents.governance.webapp.run_llm_gates_for_artifact", fake_run)

    async with asgi_client() as client:
        response = await client.post("/artifacts/art-7/run-readiness?workspace_id=ws-a")

    assert response.status_code == 502
    assert response.json() == {"detail": "run_artifact_readiness failed"}


async def test_artifact_readiness_route_requires_workspace() -> None:
    async with asgi_client() as client:
        response = await client.post("/artifacts/art-7/run-readiness")
    assert response.status_code == 422
