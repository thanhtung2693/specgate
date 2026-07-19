"""SpecGate agent package (LangGraph governance)."""

from __future__ import annotations

import os
from collections.abc import MutableMapping


def configure_telemetry_privacy_defaults(
    env: MutableMapping[str, str] | None = None,
) -> None:
    """Hide LangSmith trace payloads by default; operators may explicitly opt in."""
    target = os.environ if env is None else env
    target.setdefault("LANGSMITH_HIDE_INPUTS", "true")
    target.setdefault("LANGSMITH_HIDE_OUTPUTS", "true")


configure_telemetry_privacy_defaults()

__version__ = "0.1.0"
