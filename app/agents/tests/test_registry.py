"""Doc Registry HTTP client."""

import json

import httpx
import pytest

from specgate_agents.governance.registry.client import DocRegistryClient


def test_post_artifact_uses_mock_transport() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/artifacts"
        assert request.method == "POST"
        return httpx.Response(200, json={"id": "art-1"})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.post_artifact({"feature_id": "x", "version": "v0.1"})
    assert out["id"] == "art-1"


def test_post_artifact_unwraps_huma_body_envelope() -> None:
    """Doc Registry (Huma v2) returns artifact fields under a ``body`` key."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={"body": {"id": "art-huma", "feature_id": "checkout", "version": "v0.1"}},
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.post_artifact({"feature_id": "x", "version": "v0.1"})
    assert out["id"] == "art-huma"
    assert out["feature_id"] == "checkout"


def test_get_conflicts_query_string() -> None:
    seen: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(str(request.url))
        return httpx.Response(200, json={"conflict_state": "no_conflict", "conflicts": []})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.get_conflicts(["order-service", "ui"])
    assert out["conflict_state"] == "no_conflict"
    assert len(seen) == 1
    assert "services=order-service" in seen[0]


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


def test_no_auth_header_sent() -> None:
    """Doc Registry is internal-only (registry spec §7); no Authorization header."""
    seen_headers: list[dict] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen_headers.append(dict(request.headers))
        return httpx.Response(200, json={"id": "art-1"})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    client.post_artifact({"feature_id": "x"})
    assert "authorization" not in {k.lower() for k in seen_headers[0]}


def test_post_artifact_retries_on_5xx() -> None:
    """Governance-spec §14: publish may retry up to 3 times."""
    attempts = {"count": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        attempts["count"] += 1
        if attempts["count"] < 3:
            return httpx.Response(503, json={"error": "unavailable"})
        return httpx.Response(200, json={"id": "art-2"})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport, backoff_base_s=0.0)
    out = client.post_artifact({"feature_id": "x"})
    assert out["id"] == "art-2"
    assert attempts["count"] == 3


def test_governance_context_search_posts_json_body() -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/governance/context/search"
        assert request.method == "POST"
        captured["body"] = json.loads(request.content.decode())
        return httpx.Response(
            200,
            json={
                "results": [
                    {
                        "document_id": "doc_1",
                        "version": "v1",
                        "title": "Rules",
                        "document_type": "srs",
                        "authority_level": "high",
                        "chunk_text": "hello",
                        "score": 0.9,
                        "source_uri": "s3://x",
                        "chunk_index": 0,
                    }
                ]
            },
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.governance_context_search(
        {"query": "loyalty", "linked_feature_id": "feat-a", "max_chunks": 8}
    )
    assert captured["body"]["query"] == "loyalty"
    assert captured["body"]["linked_feature_id"] == "feat-a"
    assert out["results"][0]["chunk_text"] == "hello"


def test_list_events_uses_after_timestamp_and_unwraps_items() -> None:
    seen: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/events"
        seen["query"] = str(request.url.query)
        return httpx.Response(
            200,
            json={
                "body": {
                    "items": [
                        {
                            "id": "evt-1",
                            "artifact_id": "art-1",
                            "event_type": "artifact.needs_changes",
                            "payload": {"review_note": "fix"},
                            "created_at": "2026-04-24T01:00:00Z",
                        }
                    ]
                }
            },
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.list_events(after="2026-04-24T00:00:00Z", limit=25)
    query = seen["query"]
    if isinstance(query, bytes):
        query = query.decode()
    assert "after=2026-04-24T00%3A00%3A00Z" in query
    assert "limit=25" in query
    assert out[0]["id"] == "evt-1"


def test_get_skills_returns_items_from_paginated_response() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/skills"
        assert request.method == "GET"
        return httpx.Response(
            200,
            json={
                "items": [
                    {"id": "s1", "name": "prd-writing", "prompt": "## PRD..."},
                    {"id": "s2", "name": "spec-writing", "prompt": "## Spec..."},
                ]
            },
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    skills = client.get_skills()
    assert len(skills) == 2
    assert skills[0]["name"] == "prd-writing"


def test_get_skills_returns_list_response() -> None:
    """Some registry versions may return a bare list instead of {items: [...]}."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json=[{"id": "s1", "name": "task-breakdown", "prompt": "## Tasks"}],
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    skills = client.get_skills()
    assert skills[0]["name"] == "task-breakdown"


def test_get_skills_returns_items_from_huma_body_wrapper() -> None:
    """Some HTTP stacks wrap list payloads as ``{"body": {"items": [...]}}``."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "body": {
                    "items": [
                        {"id": "s1", "name": "prd-writing", "prompt": "## PRD..."},
                    ]
                }
            },
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    skills = client.get_skills()
    assert len(skills) == 1
    assert skills[0]["name"] == "prd-writing"


def test_get_skills_returns_items_from_huma_go_default_body_key() -> None:
    """doc-registry ``ListSkillsOutput`` uses Go's default ``Body`` JSON key."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "Body": {
                    "items": [
                        {"id": "s1", "name": "prd-writing", "prompt": "## PRD..."},
                    ]
                }
            },
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    skills = client.get_skills()
    assert len(skills) == 1
    assert skills[0]["name"] == "prd-writing"


def test_post_artifact_gives_up_after_max_retries() -> None:
    attempts = {"count": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        attempts["count"] += 1
        return httpx.Response(500, json={"error": "boom"})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient(
        "http://registry.test",
        transport=transport,
        max_retries=3,
        backoff_base_s=0.0,
    )
    try:
        client.post_artifact({"feature_id": "x"})
        raise AssertionError("expected HTTPStatusError")
    except httpx.HTTPStatusError:
        pass
    assert attempts["count"] == 3


def test_patch_artifact_status_sends_patch_request() -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["method"] = request.method
        captured["path"] = request.url.path
        captured["body"] = json.loads(request.content.decode())
        return httpx.Response(200, json={"id": "art-1", "status": "approved"})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = client.patch_artifact_status("art-1", "approved")
    assert captured["method"] == "PATCH"
    assert captured["path"] == "/artifacts/art-1/status"
    assert captured["body"]["status"] == "approved"
    assert out["status"] == "approved"


def test_patch_artifact_status_retries_on_5xx() -> None:
    attempts = {"count": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        attempts["count"] += 1
        if attempts["count"] < 3:
            return httpx.Response(503, json={"error": "unavailable"})
        return httpx.Response(200, json={"id": "art-1", "status": "approved"})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport, backoff_base_s=0.0)
    out = client.patch_artifact_status("art-1", "approved")
    assert out["status"] == "approved"
    assert attempts["count"] == 3


@pytest.mark.asyncio
async def test_aget_conflicts_query_string() -> None:
    seen: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(str(request.url))
        return httpx.Response(200, json={"conflict_state": "no_conflict", "conflicts": []})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = await client.aget_conflicts(["order-service", "ui"])
    assert out["conflict_state"] == "no_conflict"
    assert len(seen) == 1
    assert "services=order-service" in seen[0]


@pytest.mark.asyncio
async def test_aget_skills_paginated() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/skills"
        assert request.method == "GET"
        return httpx.Response(
            200,
            json={
                "items": [
                    {"id": "s1", "name": "prd-writing", "prompt": "## PRD..."},
                ]
            },
        )

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    skills = await client.aget_skills()
    assert len(skills) == 1
    assert skills[0]["name"] == "prd-writing"


@pytest.mark.asyncio
async def test_agovernance_context_search_posts_json_body() -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/governance/context/search"
        assert request.method == "POST"
        captured["body"] = json.loads(request.content.decode())
        return httpx.Response(200, json={"results": []})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = await client.agovernance_context_search({"query": "x", "max_chunks": 3})
    assert captured["body"]["query"] == "x"
    assert out == {"results": []}


@pytest.mark.asyncio
async def test_artifact_edit_session_client_methods() -> None:
    captured: list[tuple[str, str, dict | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content.decode()) if request.content else None
        captured.append((request.method, request.url.path, body))
        if request.url.path == "/artifact-edit/sessions":
            return httpx.Response(200, json={"body": {"id": "sess-1", "state": "active"}})
        if request.url.path == "/artifact-edit/sessions/sess-1/patch":
            return httpx.Response(200, json={"body": {"ok": True}})
        if request.url.path == "/artifact-edit/sessions/sess-1/files/prd":
            return httpx.Response(200, json={"body": {"ok": True}})
        if request.url.path == "/artifact-edit/sessions/sess-1/diff":
            return httpx.Response(200, json={"body": {"summary": "1 file changed"}})
        if request.url.path == "/artifact-edit/sessions/sess-1/save":
            return httpx.Response(200, json={"body": {"saved_revision_id": "rev-1"}})
        if request.url.path == "/artifacts/art-1/revisions":
            return httpx.Response(200, json={"body": {"items": [{"id": "rev-1"}]}})
        return httpx.Response(204)

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)

    session = await client.acreate_artifact_edit_session({"artifact_id": "art-1"})
    patched = await client.apatch_artifact_edit_session("sess-1", {"file_key": "prd"})
    replaced = await client.areplace_artifact_edit_file(
        "sess-1",
        "prd",
        {"content": "# PRD"},
    )
    diff = await client.aget_artifact_edit_session_diff("sess-1")
    saved = await client.asave_artifact_edit_session("sess-1", {"summary": "done"})
    revisions = await client.alist_artifact_revisions("art-1")

    assert session["id"] == "sess-1"
    assert patched["ok"] is True
    assert replaced["ok"] is True
    assert diff["summary"] == "1 file changed"
    assert saved["saved_revision_id"] == "rev-1"
    assert revisions == [{"id": "rev-1"}]
    assert ("POST", "/artifact-edit/sessions", {"artifact_id": "art-1"}) in captured


@pytest.mark.asyncio
async def test_aload_artifact_markdown_bundle_loads_qa_rollout_risks() -> None:
    """The quality-gate bundle loader requests impl/QA/rollout/risks sections too."""
    requested: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        # /artifacts/{id}/files/{key} — record the key, return no signed_url.
        key = request.url.path.rsplit("/", 1)[-1]
        requested.append(key)
        return httpx.Response(200, json={"signed_url": ""})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)

    bundle = await client.aload_artifact_markdown_bundle("art-1")

    assert set(bundle.keys()) == {
        "prd",
        "spec",
        "tasks_fe",
        "tasks_be",
        "tasks_qa",
        "rollout",
        "risks",
    }
    assert {"tasks_qa", "rollout", "risks"} <= set(requested)
    assert all(v == "" for v in bundle.values())


@pytest.mark.asyncio
async def test_aread_repo_file_returns_content_and_omits_head_ref() -> None:
    seen: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(200, json={"body": {"content": "# hello", "found": True}})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = await client.aread_repo_file("group/project", "README.md", ref="HEAD")
    assert out == "# hello"
    assert len(seen) == 1
    assert seen[0].url.path == "/repos/file"
    assert seen[0].url.params.get("project") == "group/project"
    assert seen[0].url.params.get("path") == "README.md"
    # HEAD is sent as no ref so Doc Registry uses the resource default ref.
    assert "ref" not in seen[0].url.params


@pytest.mark.asyncio
async def test_aread_repo_file_forwards_explicit_ref() -> None:
    seen: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(200, json={"body": {"content": "x", "found": True}})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    await client.aread_repo_file("group/project", "README.md", ref="develop")
    assert seen[0].url.params.get("ref") == "develop"


@pytest.mark.asyncio
async def test_aread_repo_file_found_false_returns_empty() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"body": {"content": "", "found": False}})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = await client.aread_repo_file("group/project", "MISSING.md")
    assert out == ""


@pytest.mark.asyncio
async def test_aread_repo_file_404_returns_empty() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(404, json={"detail": "unknown repo project"})

    transport = httpx.MockTransport(handler)
    client = DocRegistryClient("http://registry.test", transport=transport)
    out = await client.aread_repo_file("group/unknown", "README.md")
    assert out == ""


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
