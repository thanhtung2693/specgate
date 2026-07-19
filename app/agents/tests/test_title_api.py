"""Tests for the governance-chat thread-title HTTP API."""

from __future__ import annotations

from collections.abc import AsyncIterator, Callable
from typing import Any
from unittest.mock import AsyncMock

import httpx
import pytest

from specgate_agents.governance.title_api import (
    GenerateThreadTitleRequest,
    generate_thread_title_for_thread,
)
from specgate_agents.governance.webapp import app, get_thread_values_provider


def _provider_returning(values: dict[str, Any] | None) -> Callable:
    async def _provider(_thread_id: str) -> dict[str, Any] | None:
        return values

    async def _factory():
        return _provider

    return _factory


@pytest.fixture
async def client() -> AsyncIterator[httpx.AsyncClient]:
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://test"
    ) as value:
        yield value
    app.dependency_overrides.clear()


@pytest.mark.asyncio
async def test_generate_thread_title_uses_request_text(monkeypatch: pytest.MonkeyPatch) -> None:
    title_llm = AsyncMock(return_value="Checkout reminders")
    monkeypatch.setattr(
        "specgate_agents.governance.title_api.suggest_thread_title",
        title_llm,
    )
    monkeypatch.setattr(
        "specgate_agents.governance.title_api._hydrate_provider_keys",
        AsyncMock(),
    )

    async def values_provider(_thread_id: str) -> dict[str, Any]:
        return {"messages": [], "_thread_workspace_id": "ws-a"}

    result = await generate_thread_title_for_thread(
        "thread-1",
        GenerateThreadTitleRequest(workspace_id="ws-a", request_text="Add checkout reminders"),
        values_provider=values_provider,
    )

    assert result.thread_id == "thread-1"
    assert result.title == "Checkout reminders"
    title_llm.assert_awaited_once()


@pytest.mark.asyncio
async def test_generate_thread_title_is_idempotent_when_checkpoint_has_title() -> None:
    async def values_provider(_thread_id: str) -> dict[str, Any]:
        return {"thread_title": "Existing title", "messages": [], "_thread_workspace_id": "ws-a"}

    result = await generate_thread_title_for_thread(
        "thread-1",
        GenerateThreadTitleRequest(workspace_id="ws-a", request_text="ignored"),
        values_provider=values_provider,
    )

    assert result.title == "Existing title"


@pytest.mark.asyncio
async def test_generate_thread_title_raises_when_thread_missing() -> None:
    async def values_provider(_thread_id: str) -> dict[str, Any] | None:
        return None

    with pytest.raises(ValueError, match="thread not found"):
        await generate_thread_title_for_thread(
            "missing",
            GenerateThreadTitleRequest(workspace_id="ws-a", request_text="hi"),
            values_provider=values_provider,
        )


async def test_post_thread_title_route(
    client: httpx.AsyncClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning(
        {
            "messages": [{"type": "human", "content": "hello governance"}],
            "_thread_workspace_id": "ws-a",
        }
    )
    monkeypatch.setattr(
        "specgate_agents.governance.title_api.suggest_thread_title",
        AsyncMock(return_value="General planning"),
    )
    monkeypatch.setattr(
        "specgate_agents.governance.title_api._hydrate_provider_keys",
        AsyncMock(),
    )

    res = await client.post(
        "/governance/threads/abc/title",
        json={"workspace_id": "ws-a", "request_text": "hello governance"},
    )
    assert res.status_code == 200
    body = res.json()
    assert body["thread_id"] == "abc"
    assert body["title"] == "General planning"


async def test_post_thread_title_returns_existing_title(client: httpx.AsyncClient) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning(
        {"thread_title": "Saved title", "messages": [], "_thread_workspace_id": "ws-a"}
    )

    res = await client.post("/governance/threads/abc/title", json={"workspace_id": "ws-a"})
    assert res.status_code == 200
    assert res.json()["title"] == "Saved title"


async def test_post_thread_title_thread_not_found(client: httpx.AsyncClient) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning(None)

    res = await client.post(
        "/governance/threads/nope/title",
        json={"workspace_id": "ws-a", "request_text": "hi"},
    )
    assert res.status_code == 404


async def test_post_thread_title_rejects_workspace_mismatch(client: httpx.AsyncClient) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning(
        {"messages": [], "_thread_workspace_id": "ws-a"}
    )

    res = await client.post("/governance/threads/abc/title", json={"workspace_id": "ws-b"})
    assert res.status_code == 404


async def test_post_thread_title_requires_workspace(client: httpx.AsyncClient) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning({"messages": []})

    res = await client.post("/governance/threads/abc/title", json={"request_text": "hi"})
    assert res.status_code == 400
