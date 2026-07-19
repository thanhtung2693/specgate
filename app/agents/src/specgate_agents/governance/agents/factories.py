"""Shared model builders for governance support surfaces.

Used by the governance board surface, the governance-chat node, and the
thread-title generator.
"""

from __future__ import annotations

import os
from typing import Any

from langchain.chat_models import init_chat_model

from specgate_agents.governance.config import model
from specgate_agents.governance.constants import DEFAULT_MODEL_TEMPERATURE
from specgate_agents.governance.provider_keys import (
    governance_model_enabled,
    governance_model_provider,
    governance_thinking_level,
    provider_api_key_kwargs,
    provider_has_api_key,
)

_THINKING_LEVELS = frozenset({"low", "medium", "high"})

# Gemini thinking budget per effort level. `low` keeps a zero budget so tokens
# stream as generated (the default, no streaming regression); medium/high opt
# into server-side thinking. Values sit inside the Gemini Flash range and are
# tunable.
_GEMINI_THINKING_BUDGET = {"low": 0, "medium": 8192, "high": 24576}
# Anthropic extended-thinking budget per level. `low` disables thinking. When
# enabled, max_tokens must exceed the thinking budget and temperature must be
# left unset (the API forces it to 1).
_ANTHROPIC_THINKING_BUDGET = {"low": 0, "medium": 4096, "high": 16384}
_ANTHROPIC_ANSWER_HEADROOM = 4096


def _normalize_thinking_level(raw: str) -> str:
    if raw in _THINKING_LEVELS:
        return raw
    return "low"


def _provider_chat_kwargs(
    provider: str,
    *,
    api_key: str,
    thinking_level: str = "low",
    temperature: float = DEFAULT_MODEL_TEMPERATURE,
) -> dict:
    """Provider-specific model kwargs, including configured reasoning effort.

    `thinking_level` (low / medium / high) maps to each provider's native
    reasoning control. `low` preserves the fast, stream-friendly default for
    every provider; medium/high enable deeper reasoning where the model supports
    it and are a graceful no-op where it does not.
    """
    level = _normalize_thinking_level(thinking_level)
    kwargs = {"api_key": api_key}

    if provider == "openai":
        kwargs["temperature"] = temperature
        kwargs["reasoning_effort"] = level
        return kwargs

    if provider == "google_genai":
        kwargs["temperature"] = temperature
        kwargs["thinking_budget"] = _GEMINI_THINKING_BUDGET[level]
        return kwargs

    if provider == "anthropic":
        budget = _ANTHROPIC_THINKING_BUDGET[level]
        if budget > 0:
            kwargs["thinking"] = {"type": "enabled", "budget_tokens": budget}
            kwargs["max_tokens"] = budget + _ANTHROPIC_ANSWER_HEADROOM
        else:
            kwargs["temperature"] = temperature
        return kwargs

    if provider == "openrouter":
        kwargs["temperature"] = temperature
        # ChatOpenRouter exposes OpenRouter's native reasoning parameter
        # directly; putting it in model_kwargs/extra_body leaks that unsupported
        # kwarg into the SDK call path.
        kwargs["reasoning"] = {"effort": level}
        return kwargs

    kwargs["temperature"] = temperature
    return kwargs


def _configured_provider_chat_kwargs(
    provider: str,
    *,
    thinking_level: str = "low",
    temperature: float = DEFAULT_MODEL_TEMPERATURE,
) -> dict:
    return _provider_chat_kwargs(
        provider,
        api_key=provider_api_key_kwargs(provider)["api_key"],
        thinking_level=thinking_level,
        temperature=temperature,
    )


def build_model(*, thinking_level: str | None = None) -> Any:
    """Settings-backed server-side governance model for non-chat workloads.

    Used by gates, classifiers, quick-work criteria, and delivery review.
    Defaults the reasoning effort to the configured workspace level
    (`governance.default_thinking_level`); a caller may override per build.
    """
    provider = governance_model_provider()
    model_id = model()
    level = thinking_level if thinking_level is not None else governance_thinking_level()
    return init_chat_model(
        model_id,
        model_provider=provider,
        **_configured_provider_chat_kwargs(provider, thinking_level=level),
    )


def build_governance_ops_model() -> Any:
    """Lightweight model for the governance-chat support surface.

    Isolated from the settings-backed governance model so the chat support
    surface never shares a model instance with the evaluator that produced a
    verdict. Configured via GOVERNANCE_OPS_MODEL / GOVERNANCE_OPS_MODEL_PROVIDER
    / GOVERNANCE_OPS_API_KEY / GOVERNANCE_OPS_THINKING_LEVEL env vars; defaults
    to gpt-5.4-mini at low reasoning.
    """
    provider = os.environ.get("GOVERNANCE_OPS_MODEL_PROVIDER", "openai").strip() or "openai"
    model_id = os.environ.get("GOVERNANCE_OPS_MODEL", "gpt-5.4-mini").strip() or "gpt-5.4-mini"
    api_key = os.environ.get("GOVERNANCE_OPS_API_KEY", "").strip()
    if not api_key:
        raise RuntimeError("GOVERNANCE_OPS_API_KEY is required for the governance-ops chat model")
    level = os.environ.get("GOVERNANCE_OPS_THINKING_LEVEL", "low").strip() or "low"
    return init_chat_model(
        model_id,
        model_provider=provider,
        **_provider_chat_kwargs(provider, api_key=api_key, thinking_level=level),
    )


def ensure_llm_env() -> bool:
    """Return True if the settings-backed governance model provider's API key is present."""
    return governance_model_enabled() and provider_has_api_key(governance_model_provider())
