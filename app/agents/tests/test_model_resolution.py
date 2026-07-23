"""Single settings-backed model resolution for governance workloads.

Non-chat workloads — quick-work criteria, judges, and gates —
use one settings-backed model (`governance.model_provider` /
`governance.model`). There is no mini/main/system tier split: `build_model()`
and `ensure_llm_env()` resolve the one settings-backed governance model.
"""

from __future__ import annotations

import inspect

import pytest


@pytest.fixture(autouse=True)
def _reset_keys():
    from specgate_agents.governance.provider_keys import clear_provider_api_keys

    clear_provider_api_keys()
    yield
    clear_provider_api_keys()


def test_model_defaults_when_unset() -> None:
    from specgate_agents.governance.provider_keys import governance_model, governance_model_provider

    assert governance_model_provider() == "openai"
    assert governance_model() == "gpt-5.4-mini"


def test_model_resolves_from_settings() -> None:
    from specgate_agents.governance.provider_keys import (
        governance_model,
        governance_model_provider,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings(
        {
            "governance.model_provider": "anthropic",
            "governance.model": "claude-sonnet-4-6",
        }
    )

    assert governance_model_provider() == "anthropic"
    assert governance_model() == "claude-sonnet-4-6"


def test_unknown_model_settings_are_ignored() -> None:
    from specgate_agents.governance.provider_keys import (
        governance_model,
        governance_model_provider,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings(
        {
            "old.model_provider": "anthropic",
            "old.model": "claude-sonnet-4-6",
        }
    )

    assert governance_model_provider() == "openai"
    assert governance_model() == "gpt-5.4-mini"


def test_model_settings_are_sticky_and_ignore_blank() -> None:
    from specgate_agents.governance.provider_keys import (
        governance_model,
        governance_model_provider,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings(
        {
            "governance.model_provider": "anthropic",
            "governance.model": "claude-sonnet-4-6",
        }
    )
    # A later hydration that returns blanks must not wipe the configured values.
    set_provider_api_keys_from_settings({"governance.model_provider": "  ", "governance.model": ""})

    assert governance_model_provider() == "anthropic"
    assert governance_model() == "claude-sonnet-4-6"


def test_build_model_uses_configured_model(monkeypatch) -> None:
    from specgate_agents.governance.agents import factories

    captured: dict[str, object] = {}

    def _fake_init_chat_model(model, *, model_provider, **kwargs):  # noqa: ANN001, ANN202
        captured["model"] = model
        captured["provider"] = model_provider
        return object()

    monkeypatch.setattr(factories, "init_chat_model", _fake_init_chat_model)
    monkeypatch.setattr(factories, "governance_model_provider", lambda: "anthropic")
    monkeypatch.setattr(factories, "model", lambda: "claude-sonnet-4-6")
    monkeypatch.setattr(factories, "provider_api_key_kwargs", lambda _p: {"api_key": "sk-test"})

    factories.build_model()

    assert captured["model"] == "claude-sonnet-4-6"
    assert captured["provider"] == "anthropic"


def test_build_governance_ops_model_uses_generic_api_key_env(monkeypatch) -> None:
    from specgate_agents.governance.agents import factories

    captured: dict[str, object] = {}

    def _fake_init_chat_model(model, *, model_provider, **kwargs):  # noqa: ANN001, ANN202
        captured["model"] = model
        captured["provider"] = model_provider
        captured["kwargs"] = kwargs
        return object()

    def _should_not_read_provider_key(_provider):  # noqa: ANN001, ANN202
        raise AssertionError("governance-ops chat should use GOVERNANCE_OPS_API_KEY")

    monkeypatch.setenv("GOVERNANCE_OPS_MODEL_PROVIDER", "openrouter")
    monkeypatch.setenv("GOVERNANCE_OPS_MODEL", "openai/gpt-5.4-mini")
    monkeypatch.setenv("GOVERNANCE_OPS_API_KEY", "sk-generic")
    monkeypatch.setattr(factories, "init_chat_model", _fake_init_chat_model)
    monkeypatch.setattr(factories, "provider_api_key_kwargs", _should_not_read_provider_key)

    factories.build_governance_ops_model()

    assert captured["model"] == "openai/gpt-5.4-mini"
    assert captured["provider"] == "openrouter"
    assert captured["kwargs"]["api_key"] == "sk-generic"  # type: ignore[index]


def test_build_governance_ops_model_requires_generic_api_key_env(monkeypatch) -> None:
    from specgate_agents.governance.agents import factories

    def _should_not_read_provider_key(_provider):  # noqa: ANN001, ANN202
        raise AssertionError("governance-ops chat should not use provider-specific keys")

    monkeypatch.setenv("GOVERNANCE_OPS_MODEL_PROVIDER", "openrouter")
    monkeypatch.setenv("GOVERNANCE_OPS_MODEL", "openai/gpt-5.4-mini")
    monkeypatch.delenv("GOVERNANCE_OPS_API_KEY", raising=False)
    monkeypatch.setenv("OPENROUTER_API_KEY", "openrouter-provider-test-key")
    monkeypatch.setattr(factories, "provider_api_key_kwargs", _should_not_read_provider_key)

    with pytest.raises(RuntimeError, match="GOVERNANCE_OPS_API_KEY"):
        factories.build_governance_ops_model()


def _capture_build_kwargs(monkeypatch, *, provider, model_id, level=None) -> dict:
    """Build the model with init_chat_model stubbed; return the captured kwargs."""
    from specgate_agents.governance.agents import factories

    captured: dict[str, object] = {}

    def _fake_init_chat_model(model, *, model_provider, **kwargs):  # noqa: ANN001, ANN202
        captured["model"] = model
        captured["provider"] = model_provider
        captured["kwargs"] = kwargs
        return object()

    monkeypatch.setattr(factories, "init_chat_model", _fake_init_chat_model)
    monkeypatch.setattr(factories, "governance_model_provider", lambda: provider)
    monkeypatch.setattr(factories, "model", lambda: model_id)
    monkeypatch.setattr(factories, "provider_api_key_kwargs", lambda _p: {"api_key": "sk-test"})

    if level is None:
        factories.build_model()
    else:
        factories.build_model(thinking_level=level)
    return captured["kwargs"]  # type: ignore[return-value]


def test_provider_keys_read_thinking_level() -> None:
    from specgate_agents.governance.provider_keys import (
        governance_thinking_level,
        set_provider_api_keys_from_settings,
    )

    # Default before any hydration.
    assert governance_thinking_level() == "low"
    set_provider_api_keys_from_settings({"governance.default_thinking_level": "high"})
    assert governance_thinking_level() == "high"
    # Invalid value falls back to the default.
    set_provider_api_keys_from_settings({"governance.default_thinking_level": "bogus"})
    assert governance_thinking_level() == "low"


def test_build_model_openai_sets_reasoning_without_model_name_rules(monkeypatch) -> None:
    kwargs = _capture_build_kwargs(
        monkeypatch, provider="openai", model_id="provider-owned-model-id", level="high"
    )
    assert kwargs["reasoning_effort"] == "high"


def test_build_model_gemini_low_keeps_streaming_budget_zero(monkeypatch) -> None:
    # low is the default: Gemini stays at thinking_budget=0 so tokens stream as
    # generated — no regression for the default config.
    kwargs = _capture_build_kwargs(
        monkeypatch, provider="google_genai", model_id="gemini-3.1-flash-lite", level="low"
    )
    assert kwargs["thinking_budget"] == 0


def test_build_model_gemini_high_enables_thinking_budget(monkeypatch) -> None:
    kwargs = _capture_build_kwargs(
        monkeypatch, provider="google_genai", model_id="gemini-3.1-flash-lite", level="high"
    )
    assert kwargs["thinking_budget"] > 0


def test_build_model_anthropic_high_enables_extended_thinking(monkeypatch) -> None:
    kwargs = _capture_build_kwargs(
        monkeypatch, provider="anthropic", model_id="claude-sonnet-4-6", level="high"
    )
    assert kwargs["thinking"]["type"] == "enabled"
    assert kwargs["thinking"]["budget_tokens"] > 0
    assert kwargs["max_tokens"] > kwargs["thinking"]["budget_tokens"]
    # Extended thinking is incompatible with a custom temperature.
    assert "temperature" not in kwargs


def test_build_model_anthropic_low_keeps_temperature_no_thinking(monkeypatch) -> None:
    kwargs = _capture_build_kwargs(
        monkeypatch, provider="anthropic", model_id="claude-sonnet-4-6", level="low"
    )
    assert "thinking" not in kwargs
    assert "temperature" in kwargs


def test_build_model_openrouter_sets_reasoning_effort(monkeypatch) -> None:
    kwargs = _capture_build_kwargs(
        monkeypatch, provider="openrouter", model_id="deepseek/deepseek-v4", level="medium"
    )
    assert kwargs["reasoning"]["effort"] == "medium"
    assert "extra_body" not in kwargs


def test_build_model_uses_configured_thinking_level(monkeypatch) -> None:
    from specgate_agents.governance.provider_keys import set_provider_api_keys_from_settings

    set_provider_api_keys_from_settings({"governance.default_thinking_level": "high"})
    # No explicit level passed — build_model reads the configured workspace default.
    kwargs = _capture_build_kwargs(
        monkeypatch, provider="google_genai", model_id="gemini-3.1-flash-lite"
    )
    assert kwargs["thinking_budget"] > 0


def test_ensure_llm_env_gates_on_model_provider(monkeypatch) -> None:
    from specgate_agents.governance.agents import factories

    monkeypatch.setattr(factories, "governance_model_provider", lambda: "anthropic")
    asked: list[str] = []

    def _has_key(provider):  # noqa: ANN001, ANN202
        asked.append(provider)
        return provider == "anthropic"

    monkeypatch.setattr(factories, "provider_has_api_key", _has_key)

    assert factories.ensure_llm_env() is True
    assert asked == ["anthropic"]


def test_ensure_llm_env_respects_model_less_mode(monkeypatch) -> None:
    from specgate_agents.governance.agents import factories

    monkeypatch.setattr(factories, "governance_model_enabled", lambda: False)
    monkeypatch.setattr(factories, "provider_has_api_key", lambda _provider: True)

    assert factories.ensure_llm_env() is False


def test_readiness_gates_use_the_settings_backed_builder() -> None:
    """Readiness gates build the settings-backed governance model.

    The governance-ops chat model is isolated to the governance-chat agent; every
    verdict-producing workload uses build_model().
    """
    from specgate_agents.governance.board import quality_gates

    qg_src = inspect.getsource(quality_gates)
    assert "build_model()" in qg_src
    assert "build_mini_model" not in qg_src
    assert "build_system_model" not in qg_src
    assert "build_governance_ops_model()" not in qg_src, (
        "quality_gates must use the settings-backed build_model(), "
        "not the governance-chat support model"
    )
