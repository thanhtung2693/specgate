"""Doc Registry HTTP client."""

import json

import httpx
import pytest

from specgate_agents.governance.registry.client import DocRegistryClient


def test_get_settings_unmasked_for_governance_sends_trust_header() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/settings"
        assert request.headers["X-SpecGate-Internal-Agent"] == "governance"
        return httpx.Response(
            200,
            json={
                "body": {
                    "settings": {
                        "governance.model_provider": "google_genai",
                        "governance.model": "gemini-3.1-flash-lite",
                        "google.api_key": "secret-present",
                    }
                }
            },
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.get_settings_unmasked_for_governance()
    assert out["governance.model_provider"] == "google_genai"
    assert out["google.api_key"] == "secret-present"


@pytest.mark.asyncio
async def test_list_governance_feedback_events_scopes_workspace() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/governance/feedback-events"
        assert request.url.params["workspace_id"] == "ws-a"
        assert request.url.params["status"] == "received"
        assert request.url.params["change_request_id"] == "cr-1"
        assert request.url.params["event_type"] == "coding_agent.completed"
        return httpx.Response(200, json={"items": []})

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))
    assert (
        await client.alist_governance_feedback_events(
            workspace_id="ws-a",
            status="received",
            change_request_id="cr-1",
            event_type="coding_agent.completed",
        )
        == []
    )


@pytest.mark.asyncio
async def test_list_governance_feedback_events_rejects_blank_workspace() -> None:
    client = DocRegistryClient("http://registry.test")

    with pytest.raises(ValueError, match="workspace_id is required"):
        await client.alist_governance_feedback_events(workspace_id="  ")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "payload",
    [
        {"items": [{"name": "scope"}]},
        [{"name": "scope"}],
        {"body": {"items": [{"name": "scope"}]}},
        {"Body": {"items": [{"name": "scope"}]}},
    ],
)
async def test_aget_skills_accepts_supported_list_envelopes(payload: object) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/skills"
        assert request.url.params["workspace_id"] == "ws-a"
        return httpx.Response(200, json=payload)

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)

    assert await client.aget_skills(workspace_id="ws-a") == [{"name": "scope"}]


@pytest.mark.asyncio
async def test_alist_acceptance_criteria_preserves_canonical_ids() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert request.url.path == "/workboard/change-requests/cr-1/acceptance-criteria"
        return httpx.Response(
            200,
            json={
                "body": {
                    "items": [
                        {
                            "id": "criterion-uuid",
                            "text": "Receipt persists",
                            "verification_binding": "tests",
                        }
                    ]
                }
            },
        )

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))

    assert await client.alist_acceptance_criteria("cr-1") == [
        {
            "id": "criterion-uuid",
            "text": "Receipt persists",
            "verification_binding": "tests",
        }
    ]


@pytest.mark.asyncio
async def test_workboard_client_propagates_workspace_context() -> None:
    seen: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        if request.method == "GET":
            return httpx.Response(200, json={"id": "cr-1"})
        return httpx.Response(200, json={"id": "feature-1"})

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))
    assert client.get_change_request("cr-1", workspace_id="ws-a")["id"] == "cr-1"
    await client.aupsert_feature_by_key("FEAT-1", workspace_id="ws-a")

    assert seen[0].url.params["workspace_id"] == "ws-a"
    assert json.loads(seen[1].content.decode())["workspace_id"] == "ws-a"


def test_refresh_artifact_readiness_runs_posts_evaluations() -> None:
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["method"] = request.method
        captured["body"] = json.loads(request.content.decode())
        return httpx.Response(
            200, json={"body": {"items": [{"gate": "scope_clear", "state": "pass"}]}}
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.refresh_artifact_readiness_runs(
        "art-1",
        [{"gate": "scope_clear", "state": "pass", "confidence": 0.9}],
    )
    assert captured["path"] == "/artifacts/art-1/readiness-runs/refresh"
    assert captured["method"] == "POST"
    assert captured["body"] == {
        "evaluations": [{"gate": "scope_clear", "state": "pass", "confidence": 0.9}]
    }
    assert out == [{"gate": "scope_clear", "state": "pass"}]


def test_list_artifact_readiness_runs_reads_items() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/artifacts/art-1/readiness-runs"
        assert request.url.params.get("limit") == "10"
        return httpx.Response(200, json={"items": [{"gate": "scope_clear", "state": "pass"}]})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.list_artifact_readiness_runs("art-1", limit=10)
    assert out == [{"gate": "scope_clear", "state": "pass"}]


@pytest.mark.asyncio
async def test_search_governance_knowledge_posts_workspace_scoped_body() -> None:
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["body"] = json.loads(request.content.decode())
        return httpx.Response(
            200,
            json={
                "body": {
                    "results": [
                        {
                            "document_id": "doc-a",
                            "workspace_id": "ws-a",
                            "citation": "specgate://knowledge/ws-a/doc-a/v1#chunk-1",
                        }
                    ]
                }
            },
        )

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))

    out = await client.asearch_governance_knowledge(
        workspace_id="ws-a",
        query="release policy",
        linked_feature_id="feat-1",
        linked_request_id="cr-1",
        document_types=["policy"],
        authority_levels=["approved"],
        limit=3,
        context_mode="section",
    )

    assert captured == {
        "path": "/governance/context/search",
        "body": {
            "workspace_id": "ws-a",
            "query": "release policy",
            "linked_feature_id": "feat-1",
            "linked_request_id": "cr-1",
            "document_types": ["policy"],
            "authority_levels": ["approved"],
            "max_chunks": 3,
            "context_mode": "section",
        },
    }
    assert out["items"][0]["workspace_id"] == "ws-a"
    assert out["items"][0]["citation"] == "specgate://knowledge/ws-a/doc-a/v1#chunk-1"
