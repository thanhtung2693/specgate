"""The thin governance-chat node exposes governance-op tools and no drafting tools."""
from __future__ import annotations

from langchain_core.language_models.fake_chat_models import FakeMessagesListChatModel
from langchain_core.messages import AIMessage

from specgate_agents.governance import governance_chat


def test_governance_chat_tool_surface() -> None:
    names = governance_chat.governance_tool_names()
    assert {"get_artifact", "list_artifact_readiness", "run_artifact_readiness"} <= names
    for forbidden in ("draft_prd", "draft_spec", "draft_fe", "draft_be", "draft_qa", "read_draft"):
        assert forbidden not in names


def test_governance_chat_graph_builds(monkeypatch) -> None:
    fake = FakeMessagesListChatModel(responses=[AIMessage(content="ok")])
    monkeypatch.setattr(governance_chat, "build_governance_ops_model", lambda **_: fake)
    compiled = governance_chat.build_governance_chat_graph()
    assert compiled is not None
