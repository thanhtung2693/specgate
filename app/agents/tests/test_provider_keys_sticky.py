"""Sticky-cache behavior for provider API keys.

`set_provider_api_keys_from_settings` is called on every governance turn (per
intent middleware) and used to wipe + repopulate the in-process cache. That
made the governance-ops susceptible to silent stub fallbacks whenever Doc Registry
returned an empty body, a masked `***` response (trust header missed), or
during a momentary doc-registry restart — the cache would clear and the next
governance operation would hit `ensure_llm_env=False` with no warning.

These tests pin the sticky-update contract: a real value still overrides,
but empty / absent / masked hydrations keep the previously-loaded key.
"""

from __future__ import annotations

import pytest


@pytest.fixture(autouse=True)
def _reset_keys():
    from specgate_agents.governance.provider_keys import clear_provider_api_keys

    clear_provider_api_keys()
    yield
    clear_provider_api_keys()


def test_initial_load_stores_real_keys() -> None:
    from specgate_agents.governance.provider_keys import (
        provider_has_api_key,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings({"openai.api_key": "sk-real-1"})
    assert provider_has_api_key("openai") is True


def test_subsequent_empty_hydration_keeps_previous_key() -> None:
    from specgate_agents.governance.provider_keys import (
        provider_has_api_key,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings({"openai.api_key": "sk-real-1"})
    # Doc-registry blip: nothing came back this time.
    set_provider_api_keys_from_settings({})
    assert provider_has_api_key("openai") is True


def test_masked_hydration_keeps_previous_key() -> None:
    from specgate_agents.governance.provider_keys import (
        provider_has_api_key,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings({"openai.api_key": "sk-real-1"})
    # Trust header missed: doc-registry returned masked stars.
    set_provider_api_keys_from_settings({"openai.api_key": "***"})
    assert provider_has_api_key("openai") is True


def test_empty_string_hydration_keeps_previous_key() -> None:
    from specgate_agents.governance.provider_keys import (
        provider_has_api_key,
        set_provider_api_keys_from_settings,
    )

    set_provider_api_keys_from_settings({"openai.api_key": "sk-real-1"})
    set_provider_api_keys_from_settings({"openai.api_key": "  "})
    assert provider_has_api_key("openai") is True


def test_new_real_value_overrides_previous() -> None:
    import os

    from specgate_agents.governance.provider_keys import (
        require_provider_api_key,
        set_provider_api_keys_from_settings,
    )

    # Make sure the env-var fallback can't satisfy the lookup for this test.
    old_env = os.environ.pop("OPENAI_API_KEY", None)
    try:
        set_provider_api_keys_from_settings({"openai.api_key": "sk-real-1"})
        set_provider_api_keys_from_settings({"openai.api_key": "sk-real-2"})
        assert require_provider_api_key("openai") == "sk-real-2"
    finally:
        if old_env is not None:
            os.environ["OPENAI_API_KEY"] = old_env
