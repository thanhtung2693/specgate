"""Pytest defaults — avoid LangSmith background flush noise during unit tests."""

from __future__ import annotations

import os

# `resolve_skills_for_prompt` uses @traceable; without this, pytest can emit
# RuntimeError from LangSmith's tracing thread at interpreter shutdown.
os.environ.setdefault("LANGSMITH_TRACING_V2", "false")
