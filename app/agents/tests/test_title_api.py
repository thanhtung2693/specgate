"""Tests for the governance-chat thread-title HTTP API."""

from __future__ import annotations

from collections.abc import Callable
from typing import Any
from unittest.mock import AsyncMock

import pytest
from fastapi.testclient import TestClient

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
def client():
    yield TestClient(app)
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
        return {"messages": []}

    result = await generate_thread_title_for_thread(
        "thread-1",
        GenerateThreadTitleRequest(request_text="Add checkout reminders"),
        values_provider=values_provider,
    )

    assert result.thread_id == "thread-1"
    assert result.title == "Checkout reminders"
    title_llm.assert_awaited_once()


@pytest.mark.asyncio
async def test_generate_thread_title_is_idempotent_when_checkpoint_has_title() -> None:
    async def values_provider(_thread_id: str) -> dict[str, Any]:
        return {"thread_title": "Existing title", "messages": []}

    result = await generate_thread_title_for_thread(
        "thread-1",
        GenerateThreadTitleRequest(request_text="ignored"),
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
            GenerateThreadTitleRequest(request_text="hi"),
            values_provider=values_provider,
        )


def test_post_thread_title_route(client: TestClient, monkeypatch: pytest.MonkeyPatch) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning(
        {"messages": [{"type": "human", "content": "hello governance"}]}
    )
    monkeypatch.setattr(
        "specgate_agents.governance.title_api.suggest_thread_title",
        AsyncMock(return_value="General planning"),
    )
    monkeypatch.setattr(
        "specgate_agents.governance.title_api._hydrate_provider_keys",
        AsyncMock(),
    )

    res = client.post(
        "/governance/threads/abc/title",
        json={"request_text": "hello governance"},
    )
    assert res.status_code == 200
    body = res.json()
    assert body["thread_id"] == "abc"
    assert body["title"] == "General planning"


def test_post_thread_title_returns_existing_title(client: TestClient) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning(
        {"thread_title": "Saved title", "messages": []}
    )

    res = client.post("/governance/threads/abc/title", json={})
    assert res.status_code == 200
    assert res.json()["title"] == "Saved title"


def test_post_thread_title_thread_not_found(client: TestClient) -> None:
    app.dependency_overrides[get_thread_values_provider] = _provider_returning(None)

    res = client.post("/governance/threads/nope/title", json={"request_text": "hi"})
    assert res.status_code == 404
