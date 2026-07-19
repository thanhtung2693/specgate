"""HTTP handler for LLM thread titles — outside the LangGraph message stream."""

from __future__ import annotations

import logging
from collections.abc import Awaitable, Callable
from typing import Any

from pydantic import BaseModel, Field

from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.intent.title import suggest_thread_title
from specgate_agents.governance.provider_keys import (
    governance_model_provider,
    provider_has_api_key,
    set_provider_api_keys_from_settings,
)
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)

ThreadValuesProvider = Callable[[str], Awaitable[dict[str, Any] | None]]


class ThreadTitleRequestError(ValueError):
    """The caller supplied invalid title-generation context."""


class ThreadTitleNotFoundError(ValueError):
    """The requested thread is absent or outside the selected workspace."""


class GenerateThreadTitleRequest(BaseModel):
    workspace_id: str = Field(
        default="",
        description="Trusted active workspace context supplied by the runtime, not the model.",
    )
    request_text: str | None = Field(
        default=None,
        description="First user message text. When omitted, read from checkpoint messages.",
    )
    request_type: str = Field(default="unknown")


class GenerateThreadTitleResponse(BaseModel):
    thread_id: str
    title: str


def _first_human_text_from_values(values: dict[str, Any]) -> str:
    messages = values.get("messages")
    if not isinstance(messages, list):
        return ""
    for message in messages:
        if not isinstance(message, dict):
            continue
        msg_type = str(message.get("type") or message.get("role") or "").lower()
        if msg_type not in {"human", "user"}:
            continue
        content = message.get("content")
        if isinstance(content, str) and content.strip():
            return content.strip()
        if isinstance(content, list):
            parts: list[str] = []
            for part in content:
                if isinstance(part, dict) and part.get("type") == "text":
                    text = part.get("text")
                    if isinstance(text, str) and text.strip():
                        parts.append(text.strip())
            if parts:
                return "\n".join(parts)
    return ""


async def _hydrate_provider_keys() -> None:
    base_url = doc_registry_base_url()
    if not base_url:
        return
    if provider_has_api_key(governance_model_provider()):
        return
    try:
        settings = await DocRegistryClient(base_url).aget_settings_unmasked_for_governance()
    except Exception as exc:
        logger.warning("thread_title_api: provider hydration failed: %s", exc)
        return
    set_provider_api_keys_from_settings(settings)


async def generate_thread_title_for_thread(
    thread_id: str,
    body: GenerateThreadTitleRequest,
    *,
    values_provider: ThreadValuesProvider,
) -> GenerateThreadTitleResponse:
    workspace_id = body.workspace_id.strip()
    if not workspace_id:
        raise ThreadTitleRequestError("workspace_id is required")
    values = await values_provider(thread_id)
    if values is None:
        raise ThreadTitleNotFoundError("thread not found")

    thread_workspace = str(values.get("_thread_workspace_id") or "").strip()
    if not thread_workspace:
        raise ThreadTitleNotFoundError("workspace_id is missing on thread")
    if thread_workspace and thread_workspace != workspace_id:
        raise ThreadTitleNotFoundError("workspace mismatch")

    existing = str(values.get("thread_title") or "").strip()
    if existing:
        return GenerateThreadTitleResponse(thread_id=thread_id, title=existing)

    request_text = (body.request_text or "").strip() or _first_human_text_from_values(values)
    if not request_text:
        return GenerateThreadTitleResponse(thread_id=thread_id, title="")

    await _hydrate_provider_keys()
    try:
        title = await suggest_thread_title(
            request_text,
            request_type=body.request_type or "unknown",
            force=True,
            config=None,
        )
    except Exception as exc:
        logger.warning("thread_title_api: suggest_thread_title failed for %s: %s", thread_id, exc)
        title = ""

    return GenerateThreadTitleResponse(thread_id=thread_id, title=(title or "").strip())
