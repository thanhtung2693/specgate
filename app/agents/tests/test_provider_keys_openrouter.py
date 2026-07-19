"""OpenRouter is a first-class governance model provider.

OpenRouter is OpenAI-compatible and exposes a large catalog under `vendor/model`
slugs. These tests pin that the provider-key plumbing treats `openrouter` like
the other providers: a key under `openrouter.api_key` hydrates the cache, and a
governance tier can select the `openrouter` provider with a slug model.
"""

from __future__ import annotations

import pytest


@pytest.fixture(autouse=True)
def _reset_keys():
    from specgate_agents.governance.provider_keys import clear_provider_api_keys

    clear_provider_api_keys()
    yield
    clear_provider_api_keys()


def test_openrouter_api_key_hydrates_from_settings() -> None:
    from specgate_agents.governance.provider_keys import (
        provider_has_api_key,
        require_provider_api_key,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings({"openrouter.api_key": "openrouter-test-key"})
    assert provider_has_api_key("openrouter") is True
    assert require_provider_api_key("openrouter") == "openrouter-test-key"


def test_openrouter_env_fallback(monkeypatch: pytest.MonkeyPatch) -> None:
    from specgate_agents.governance.provider_keys import provider_has_api_key

    monkeypatch.setenv("OPENROUTER_API_KEY", "openrouter-env-test-key")
    assert provider_has_api_key("openrouter") is True


def test_governance_tier_selects_openrouter_with_slug_model() -> None:
    from specgate_agents.governance.provider_keys import (
        governance_model,
        governance_model_provider,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings(
        {
            "openrouter.api_key": "openrouter-test-key",
            "governance.model_provider": "openrouter",
            "governance.model": "deepseek/deepseek-v4-flash",
        }
    )
    assert governance_model_provider() == "openrouter"
    assert governance_model() == "deepseek/deepseek-v4-flash"
