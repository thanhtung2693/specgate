"""LLM-backed thread title generation for governance runs."""

from __future__ import annotations

from langchain_core.messages import AIMessage, HumanMessage
from langchain_core.runnables import RunnableConfig

from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
from specgate_agents.governance.llm_structured import isolated_structured_output_config


def _normalize_thread_title(raw: str) -> str:
    title = " ".join((raw or "").strip().split())
    title = title.strip(" \"'`~")
    return title[:72].strip()


async def suggest_thread_title(
    request_text: str,
    *,
    request_type: str = "unknown",
    current_title: str | None = None,
    force: bool = False,
    config: RunnableConfig | None = None,
) -> str:
    """Generate a concise display title for the current governance-chat thread.

    When ``force=True``, always run the LLM (used by the title HTTP API).
    """
    existing = (current_title or "").strip()
    if existing and not force:
        return existing

    text = (request_text or "").strip()
    if not text:
        return ""

    if not ensure_llm_env():
        return ""

    prompt = (
        "Write a concise display title for a software planning thread.\n"
        "Rules:\n"
        "- 2 to 6 words.\n"
        "- No quotes, no markdown, no trailing punctuation.\n"
        "- Do not include feature, request, thread, or any internal ID.\n"
        "- Keep domain terms and product names intact.\n"
        "- Return only the title.\n\n"
        f"Request type: {request_type}\n"
        f"Request:\n{text[:1000]}"
    )
    try:
        model = build_model()
        invoke_config = isolated_structured_output_config(
            dict(config) if config else None,
        )
        result = await model.ainvoke([HumanMessage(content=prompt)], config=invoke_config)
        raw = result.content if isinstance(result, AIMessage) else getattr(result, "content", "")
        if isinstance(raw, list):
            # Some providers return content as a list of parts; stitch text segments.
            raw = "".join(
                part.get("text", "") if isinstance(part, dict) else str(part) for part in raw
            )
        title = _normalize_thread_title(str(raw or ""))
        return title
    except Exception:  # noqa: BLE001
        return ""
