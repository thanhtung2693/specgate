"""Structured-output callback isolation tests."""

from specgate_agents.governance.llm_structured import (
    _supports_structured_output_method,
    isolated_structured_output_config,
)


def test_isolated_config_clears_callbacks() -> None:
    parent = {"callbacks": ["parent-cb"], "tags": ["graph"]}
    out = isolated_structured_output_config(parent)
    assert out["callbacks"] == []
    assert out["tags"] == ["graph"]


def test_isolated_config_without_parent() -> None:
    assert isolated_structured_output_config() == {"callbacks": []}


def test_structured_output_method_support_uses_capability_not_model_name() -> None:
    class Model:
        def with_structured_output(self, schema, *, method=None):  # noqa: ANN001, ANN202
            return schema, method

    assert _supports_structured_output_method(Model())


def test_structured_output_method_support_is_optional() -> None:
    class Model:
        def with_structured_output(self, schema):  # noqa: ANN001, ANN202
            return schema

    assert not _supports_structured_output_method(Model())
