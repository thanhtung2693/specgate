"""Structured-output callback isolation tests."""

from specgate_agents.governance.llm_structured import isolated_structured_output_config


def test_isolated_config_clears_callbacks() -> None:
    parent = {"callbacks": ["parent-cb"], "tags": ["graph"]}
    out = isolated_structured_output_config(parent)
    assert out["callbacks"] == []
    assert out["tags"] == ["graph"]


def test_isolated_config_without_parent() -> None:
    assert isolated_structured_output_config() == {"callbacks": []}
