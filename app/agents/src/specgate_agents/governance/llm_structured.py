"""Shared LangChain structured-output helpers for governance mini-model calls."""

from __future__ import annotations

import inspect
from typing import Any

from langchain_core.messages import BaseMessage
from pydantic import BaseModel


def _supports_structured_output_method(model: Any) -> bool:
    """Whether this model exposes the structured-output method selector."""
    try:
        params = inspect.signature(model.with_structured_output).parameters
    except (TypeError, ValueError):
        return False
    return "method" in params


def _ainvoke_accepts_config(runnable: Any) -> bool:
    """Return whether ``runnable.ainvoke`` accepts a LangChain config kwarg."""
    try:
        params = inspect.signature(runnable.ainvoke).parameters
    except (AttributeError, TypeError, ValueError):
        return True
    return "config" in params or any(
        param.kind is inspect.Parameter.VAR_KEYWORD for param in params.values()
    )


def isolated_structured_output_config(
    parent_config: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Drop parent streaming callbacks so structured-output calls do not emit messages-tuple."""
    if not parent_config:
        return {"callbacks": []}
    return {**parent_config, "callbacks": []}


async def structured_output_ainvoke[T: BaseModel](
    model: Any,
    schema: type[T],
    messages: list[BaseMessage],
    config: dict[str, Any] | None = None,
    *,
    isolate_callbacks: bool = True,
) -> T:
    """Run ``model.with_structured_output(schema).ainvoke(messages)``.

    Callers wrap in ``try/except`` to apply node-specific fallbacks.
    By default structured-output calls isolate callbacks so classifier and
    parser nodes do not leak token streams into the LangGraph SSE channel.
    """
    run_config = isolated_structured_output_config(config) if isolate_callbacks else config
    if _supports_structured_output_method(model):
        structured = model.with_structured_output(schema, method="function_calling")
    else:
        structured = model.with_structured_output(schema)
    if run_config is not None and _ainvoke_accepts_config(structured):
        out = await structured.ainvoke(messages, config=run_config)
    else:
        out = await structured.ainvoke(messages)
    return out  # type: ignore[return-value]
