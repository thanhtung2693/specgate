"""Pytest defaults — avoid LangSmith background flush noise during unit tests."""

from __future__ import annotations

import os
from unittest.mock import AsyncMock

import pytest

# `resolve_skills_for_prompt` uses @traceable; without this, pytest can emit
# RuntimeError from LangSmith's tracing thread at interpreter shutdown.
os.environ.setdefault("LANGSMITH_TRACING_V2", "false")


@pytest.fixture(autouse=True)
def _stub_thread_title_api(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep unit tests offline unless a test opts into title generation."""
    monkeypatch.setattr(
        "specgate_agents.governance.title_api.suggest_thread_title",
        AsyncMock(return_value=""),
    )
