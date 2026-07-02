"""Model + provider-key builders for the governance contract eval harness.

Provider keys hydrate the same way as ``langgraph.json``-loaded production
graphs — through ``provider_keys.require_provider_api_key`` — so the live
contract judge can call the same model the governance-ops uses.

This file does not own scoring or dataset loading — see ``harness.py``.
"""

from __future__ import annotations

from typing import Literal

from langchain_core.language_models.chat_models import BaseChatModel

EvalTarget = Literal[
    "quality_gate_contract",
    "ac_satisfaction_contract",
]

# Reserved exit code for "env not configured" (provider keys missing).
# Distinct from 1 (test failures) and 2 (CLI usage errors).
PROVIDER_KEYS_READY_EXIT_CODE = 78


class SubAgentBuildError(RuntimeError):
    """Raised when the eval model cannot be constructed (missing keys, etc.)."""


def provider_keys_available() -> tuple[bool, list[str]]:
    """Return ``(ready, missing_providers)``.

    ``ready`` is true when the provider the governance model points at has an
    API key available (cached or via env var). The list of missing providers
    is included so the CLI can print an actionable error message.
    """
    from specgate_agents.governance.provider_keys import (
        governance_model_provider,
        provider_has_api_key,
    )

    needed = {governance_model_provider()}
    missing = [p for p in needed if not provider_has_api_key(p)]
    return (not missing, missing)


def hydrate_provider_keys() -> None:
    """Hydrate provider keys from Doc Registry (best-effort)."""
    from specgate_agents.governance.provider_keys import _hydrate_from_doc_registry_sync

    try:
        _hydrate_from_doc_registry_sync()
    except Exception:
        return


def build_main_model() -> BaseChatModel:
    """Construct the governance-ops model via the production path."""
    try:
        from specgate_agents.governance.graph import _build_chat_model

        from specgate_agents.governance.provider_keys import (
            governance_model,
            governance_model_provider,
        )

        return _build_chat_model(governance_model_provider(), governance_model())
    except Exception as exc:  # pragma: no cover
        raise SubAgentBuildError(f"model: {exc}") from exc


# The app runs a single model; the harness keeps both builder names so the
# existing eval rows that ask for a "mini" model resolve to the same model.
build_mini_model = build_main_model


__all__ = [
    "EvalTarget",
    "PROVIDER_KEYS_READY_EXIT_CODE",
    "SubAgentBuildError",
    "build_main_model",
    "build_mini_model",
    "hydrate_provider_keys",
    "provider_keys_available",
]
