"""Supervisor factory — composes a deep agent from supplied dependencies.

Returns a compiled LangGraph runnable. Callers (production graph factory,
tests) inject the model, the tool set, sub-agents, ``interrupt_on``
config, and checkpointer. Middleware order is determined by the caller
(see ``graph.py``) — this factory just threads the supplied middleware
list through to ``create_deep_agent``.

Sub-agent ``response_format`` binding, ``SkillsMiddleware`` per sub-agent,
and the production system prompt are handled by the caller — this
factory does not assume a particular prompt or tool catalog.
"""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from typing import Any

from deepagents import (
    AsyncSubAgent,
    CompiledSubAgent,
    SubAgent,
    create_deep_agent,
)
from deepagents.middleware.async_subagents import AsyncSubAgentMiddleware
from langchain.agents.middleware import AgentMiddleware
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.tools import BaseTool


def build_supervisor(
    *,
    model: BaseChatModel,
    system_prompt: str,
    tools: Sequence[BaseTool] | None = None,
    subagents: Sequence[SubAgent | CompiledSubAgent] | None = None,
    async_subagents: Sequence[AsyncSubAgent] | None = None,
    middleware: Sequence[AgentMiddleware] | None = None,
    interrupt_on: Mapping[str, Any] | None = None,
    checkpointer: Any | None = None,
    name: str = "governance_supervisor",
) -> Any:
    """Compile a deep-agent supervisor.

    ``interrupt_on`` keys are tool names; values are LangChain's allowed-
    decisions dict (``{"allowed_decisions": ["approve", "reject"]}``) or
    ``True`` / ``False`` per the deepagents HITL API.

    ``async_subagents`` — when non-empty, an ``AsyncSubAgentMiddleware``
    is appended to the middleware list. It contributes the 5-tool surface
    (``start_async_task`` / ``check_async_task`` / ``update_async_task``
    / ``cancel_async_task`` / ``list_async_tasks``) plus the
    ``async_tasks`` state channel that survives summarization compaction
    (per design §4A.4). Each entry is a ``deepagents.AsyncSubAgent``
    TypedDict carrying ``name``, ``description``, ``graph_id`` and an
    optional ``url`` (omit for ASGI in-process transport).
    """
    middleware_list: list[AgentMiddleware] = list(middleware or ())
    if async_subagents:
        middleware_list.append(AsyncSubAgentMiddleware(async_subagents=list(async_subagents)))
    return create_deep_agent(
        model=model,
        system_prompt=system_prompt,
        tools=list(tools or ()),
        subagents=list(subagents or ()),
        middleware=middleware_list,
        interrupt_on=dict(interrupt_on or {}),
        checkpointer=checkpointer,
        name=name,
    )


__all__ = ["build_supervisor"]
