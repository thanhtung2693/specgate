"""HTTP tests for the governance custom-route app."""

from __future__ import annotations

import httpx
import pytest

from specgate_agents.governance.provider_keys import clear_provider_api_keys, provider_has_api_key
from specgate_agents.governance.webapp import _hydrate_provider_keys_on_startup, app


@pytest.mark.asyncio
async def test_startup_hydrates_provider_keys(monkeypatch: pytest.MonkeyPatch) -> None:
    from specgate_agents.governance import webapp as webapp_mod

    clear_provider_api_keys()
    monkeypatch.setattr(webapp_mod, "doc_registry_base_url", lambda: "http://registry.test")
    monkeypatch.setattr(webapp_mod, "provider_has_api_key", lambda _provider: False)

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            assert base_url == "http://registry.test"

        async def aget_settings_unmasked_for_governance(self) -> dict[str, str]:
            return {
                "governance.model_provider": "google_genai",
                "governance.model": "gemini-3.1-flash-lite",
                "google.api_key": "secret-present",
            }

    monkeypatch.setattr(webapp_mod, "DocRegistryClient", FakeClient)

    await _hydrate_provider_keys_on_startup()

    assert provider_has_api_key("google_genai") is True
    clear_provider_api_keys()


@pytest.mark.asyncio
async def test_chat_health_reports_unconfigured_without_key(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from specgate_agents.governance.webapp import chat_health

    monkeypatch.delenv("GOVERNANCE_OPS_API_KEY", raising=False)
    monkeypatch.setenv("GOVERNANCE_OPS_MODEL", "gpt-5.4-mini")
    monkeypatch.setenv("GOVERNANCE_OPS_MODEL_PROVIDER", "openai")

    payload = await chat_health()

    assert payload == {"configured": False, "provider": "openai", "model": "gpt-5.4-mini"}


@pytest.mark.asyncio
async def test_chat_health_reports_configured_with_key(monkeypatch: pytest.MonkeyPatch) -> None:
    from specgate_agents.governance.webapp import chat_health

    monkeypatch.setenv("GOVERNANCE_OPS_API_KEY", "secret")

    payload = await chat_health()

    assert payload["configured"] is True
    assert "secret" not in str(payload)


async def test_custom_webapp_does_not_shadow_langgraph_thread_search() -> None:
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://test"
    ) as client:
        res = await client.post(
            "/threads/search",
            json={
                "metadata": {"source": "specgate-ui", "surface": "governance-agent"},
                "limit": 20,
            },
        )

    assert res.status_code == 404


def test_custom_webapp_does_not_expose_thread_title_management() -> None:
    assert "/governance/threads/{thread_id}/title" not in {route.path for route in app.routes}
