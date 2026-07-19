"""Governance model provider credentials loaded from Doc Registry settings."""

from __future__ import annotations

import concurrent.futures
import logging
import os
import threading
from typing import Literal

logger = logging.getLogger(__name__)

GovernanceModelProvider = Literal["openai", "google_genai", "anthropic", "openrouter"]

_SETTING_TO_PROVIDER: dict[str, GovernanceModelProvider] = {
    "openai.api_key": "openai",
    "google.api_key": "google_genai",
    "anthropic.api_key": "anthropic",
    "openrouter.api_key": "openrouter",
}

_PROVIDER_LABELS: dict[GovernanceModelProvider, str] = {
    "openai": "OpenAI",
    "google_genai": "Google",
    "anthropic": "Anthropic",
    "openrouter": "OpenRouter",
}

_PROVIDER_ENV_KEYS: dict[GovernanceModelProvider, str] = {
    "openai": "OPENAI_API_KEY",
    "google_genai": "GOOGLE_API_KEY",
    "anthropic": "ANTHROPIC_API_KEY",
    "openrouter": "OPENROUTER_API_KEY",
}

_provider_api_keys: dict[GovernanceModelProvider, str] = {}
# Settings-backed model for server-side governance workloads (quick-work
# criteria, judges, and gates). Governance chat uses a separate env-configured
# support model.
_governance_model_provider: GovernanceModelProvider = "openai"
_governance_model_name = "gpt-5.4-mini"
_governance_model_enabled = True
_sync_hydration_local = threading.local()

# Runtime policy / operational settings (user-configurable in the UI "General"
# tab). Defaults mirror the in-code constants these settings replace; hydration
# overrides them, invalid/blank values keep the default.
_GATE_CONFIDENCE_THRESHOLD_DEFAULT = 0.7
_REGISTRY_TIMEOUT_SECONDS_DEFAULT = 30.0
# Reasoning effort applied to the governance model (OpenAI reasoning_effort / Gemini
# thinking budget / Anthropic extended thinking). `low` keeps the fast,
# stream-friendly default; medium/high opt into deeper reasoning.
_THINKING_LEVELS = frozenset({"low", "medium", "high"})
_DEFAULT_THINKING_LEVEL = "low"

_gate_confidence_threshold = _GATE_CONFIDENCE_THRESHOLD_DEFAULT
_registry_timeout_seconds = _REGISTRY_TIMEOUT_SECONDS_DEFAULT
_governance_thinking_level = _DEFAULT_THINKING_LEVEL


def _coerce_thinking_level(raw: object) -> str:
    value = str(raw or "").strip().lower()
    return value if value in _THINKING_LEVELS else _DEFAULT_THINKING_LEVEL


def _coerce_float(raw: object, default: float) -> float:
    value = str(raw or "").strip()
    if not value:
        return default
    try:
        return float(value)
    except ValueError:
        return default


def _hydrate_from_doc_registry_sync() -> None:
    """Best-effort sync hydration for host / graph-factory call sites.

    Runs the HTTP fetch in a worker thread so an event loop on the caller's
    thread (LangGraph dev runtime) does not flag sync I/O as a blocking
    violation. The caller blocks briefly on the future; subsequent calls
    short-circuit on the populated cache.
    """
    if getattr(_sync_hydration_local, "active", False):
        return
    from specgate_agents.governance.config import doc_registry_base_url
    from specgate_agents.governance.registry.client import DocRegistryClient

    base_url = doc_registry_base_url()
    if not base_url:
        logger.warning(
            "provider_settings_hydration: DOC_REGISTRY_BASE_URL is empty; cannot pull provider keys"
        )
        return

    def _fetch() -> dict[str, str]:
        return DocRegistryClient(base_url).get_settings_unmasked_for_governance()

    try:
        _sync_hydration_local.active = True
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
            future = executor.submit(_fetch)
            settings = future.result(timeout=15.0)
    except Exception as exc:
        logger.warning("provider_settings_hydration: fetch failed for %s: %s", base_url, exc)
        return
    finally:
        _sync_hydration_local.active = False
    set_provider_api_keys_from_settings(settings)
    populated_count = len(_provider_api_keys)
    if populated_count:
        logger.info("provider_settings_hydration: cached keys for %d provider(s)", populated_count)
    else:
        logger.warning(
            "provider_settings_hydration: %s returned no usable provider keys",
            base_url,
        )


async def hydrate_governance_model_settings() -> None:
    """Best-effort async hydration of governance model settings + provider keys.

    Pulls unmasked settings from Doc Registry and applies them to the in-process
    provider-key cache. Used by the workboard ASGI routes (quality gates, route
    suggestions) before invoking an LLM. Failures are
    swallowed (logged) so a transient registry blip never blocks the request.
    """
    from specgate_agents.governance.config import doc_registry_base_url
    from specgate_agents.governance.registry.client import DocRegistryClient

    base_url = doc_registry_base_url()
    if not base_url:
        return
    try:
        settings = await DocRegistryClient(base_url).aget_settings_unmasked_for_governance()
    except Exception:
        logger.warning("governance model settings hydration failed", exc_info=True)
        return
    set_provider_api_keys_from_settings(settings)


def set_provider_api_keys_from_settings(settings: dict[str, str]) -> None:
    """Replace governance model defaults and provider API keys from settings."""
    global _governance_model_name, _governance_model_provider, _governance_model_enabled

    enabled = str(settings.get("governance.model_enabled") or "true").strip().lower()
    _governance_model_enabled = enabled != "false"

    provider = str(settings.get("governance.model_provider") or "").strip()
    if provider in _PROVIDER_LABELS:
        _governance_model_provider = provider  # type: ignore[assignment]
    model = str(settings.get("governance.model") or "").strip()
    if model:
        _governance_model_name = model

    _apply_runtime_policy_settings(settings)

    # Sticky update: only override an existing entry when hydration brings a
    # real new value. Earlier code unconditionally cleared the cache and
    # repopulated, which meant a single hydration that returned an empty body
    # (transient doc-registry blip, masked `***` response on a request that
    # missed the trust header, etc.) wiped the keys for the rest of the turn
    # — so the next governance operation hit `ensure_llm_env=False` and silently
    # produced a stub. Holding the previous value across an empty hydration
    # keeps governance operations usable through brief outages.
    for setting_key, provider in _SETTING_TO_PROVIDER.items():
        raw = settings.get(setting_key)
        if raw is None:
            # Field absent: keep whatever we already have.
            continue
        value = str(raw).strip()
        if not value:
            # Field explicitly empty: also treat as a no-op for the cache.
            # An operator who genuinely wants to remove a key uses
            # `clear_provider_api_keys()` (tests) or restarts the process.
            continue
        if value == "***":
            # Masked response — almost always a transient header issue.
            # Don't drop a previously-good key on the floor; log it instead.
            if provider in _provider_api_keys:
                logger.warning(
                    "provider_settings_hydration: %s masked (***); keeping cached key",
                    setting_key,
                )
            continue
        _provider_api_keys[provider] = value


def _apply_runtime_policy_settings(settings: dict[str, str]) -> None:
    """Update the runtime policy/operational globals from a settings dict.

    Each value falls back to its default when absent, blank, or unparseable —
    so a partial or transient hydration never drops a knob to a wrong value.
    """
    global _gate_confidence_threshold
    global _registry_timeout_seconds
    global _governance_thinking_level

    _gate_confidence_threshold = _coerce_float(
        settings.get("governance.gate_confidence_threshold"),
        _GATE_CONFIDENCE_THRESHOLD_DEFAULT,
    )
    _governance_thinking_level = _coerce_thinking_level(
        settings.get("governance.default_thinking_level")
    )
    _registry_timeout_seconds = _coerce_float(
        settings.get("governance.registry_timeout_seconds"),
        _REGISTRY_TIMEOUT_SECONDS_DEFAULT,
    )


def governance_gate_confidence_threshold() -> float:
    return _gate_confidence_threshold


def governance_registry_timeout_seconds() -> float:
    return _registry_timeout_seconds


def governance_thinking_level() -> str:
    """Configured reasoning effort (low / medium / high) for the governance model."""
    return _governance_thinking_level


def clear_provider_api_keys() -> None:
    """Clear the in-process settings cache; intended for tests."""
    global _governance_model_name, _governance_model_provider, _governance_model_enabled
    global _gate_confidence_threshold
    global _registry_timeout_seconds
    global _governance_thinking_level

    _provider_api_keys.clear()
    _governance_model_provider = "openai"
    _governance_model_name = "gpt-5.4-mini"
    _governance_model_enabled = True
    _gate_confidence_threshold = _GATE_CONFIDENCE_THRESHOLD_DEFAULT
    _registry_timeout_seconds = _REGISTRY_TIMEOUT_SECONDS_DEFAULT
    _governance_thinking_level = _DEFAULT_THINKING_LEVEL


def governance_model_provider() -> GovernanceModelProvider:
    return _governance_model_provider


def governance_model_enabled() -> bool:
    return _governance_model_enabled


def governance_model() -> str:
    return _governance_model_name


def provider_has_api_key(provider: str) -> bool:
    key = _provider_api_keys.get(provider)  # type: ignore[arg-type]
    if key:
        return True
    env_key = _PROVIDER_ENV_KEYS.get(provider)  # type: ignore[arg-type]
    return bool(env_key and os.getenv(env_key))


def require_provider_api_key(provider: str) -> str:
    key = _provider_api_keys.get(provider)  # type: ignore[arg-type]
    if not key:
        _hydrate_from_doc_registry_sync()
        key = _provider_api_keys.get(provider)  # type: ignore[arg-type]
    if not key:
        env_key = _PROVIDER_ENV_KEYS.get(provider)  # type: ignore[arg-type]
        key = os.getenv(env_key or "")
    if key:
        return key
    label = _PROVIDER_LABELS.get(provider, provider)  # type: ignore[arg-type]
    raise RuntimeError(
        f"{label} API key is missing in Doc Registry model settings. "
        "Open the Governance model settings modal and save the provider key before "
        "running this model."
    )


def provider_api_key_kwargs(provider: str) -> dict[str, str]:
    return {"api_key": require_provider_api_key(provider)}
