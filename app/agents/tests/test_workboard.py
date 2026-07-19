from __future__ import annotations

import json

import httpx

from specgate_agents.governance.registry.client import DocRegistryClient
from specgate_agents.governance.webapp import app


def test_doc_registry_client_gate_runs_refresh_posts_evaluations() -> None:
    seen: list[tuple[str, str, dict | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read()
        seen.append((request.method, request.url.path, None if not body else json.loads(body)))
        if request.url.path == "/workboard/change-requests/cr-1/gate-runs/refresh":
            return httpx.Response(
                200, json={"body": {"items": [{"gate": "rollback_plan_present", "state": "pass"}]}}
            )
        if request.url.path == "/workboard/change-requests/cr-1/gate-runs":
            return httpx.Response(
                200, json={"body": {"items": [{"gate": "rollback_plan_present", "state": "pass"}]}}
            )
        return httpx.Response(404)

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))
    evals = [{"gate": "rollback_plan_present", "state": "needs_human_review", "confidence": 0.5}]
    rows = client.refresh_change_request_gate_runs("cr-1", evals)
    assert rows and rows[0]["gate"] == "rollback_plan_present"

    listed = client.list_change_request_gate_runs("cr-1", limit=10)
    assert listed and listed[0]["state"] == "pass"

    post = next(s for s in seen if s[1].endswith("/gate-runs/refresh"))
    assert post[0] == "POST"
    assert post[2] == {"evaluations": evals}

    client.refresh_change_request_gate_runs("cr-1", evals, evaluations_only=True)
    assert seen[-1][2] == {"evaluations": evals, "evaluations_only": True}


def test_route_classification_endpoint_is_not_registered() -> None:
    assert "/workboard/change-requests/{change_request_id}/classify-route" not in {
        route.path for route in app.routes
    }
