"""Quick-route CR creation from IDE issue content.

Accepts tracker issue content the developer already has (title, description,
optional issue URL/key, optional feature key) and creates a governed quick-route
CR — without SpecGate reaching into the tracker directly.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from pydantic import BaseModel

from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.llm_structured import structured_output_ainvoke
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)

_AC_DRAFT_SYSTEM_PROMPT = """
You draft acceptance criteria for a quick-route software change request.

Rules:
- Return 3-8 concrete, testable criteria. Each criterion is a single sentence
  a developer can verify independently.
- Focus on observable outcomes, not implementation steps.
- Use explicit success conditions, failure cases, and non-goals from the description.
- Do not invent missing requirements. If title and description do not provide
  enough observable behavior for concrete criteria, return an empty list.
- Do not include meta-criteria like "write a test for X" — phrase as verifiable behavior.
"""


class _ACList(BaseModel):
    acceptance_criteria: list[str]


def _normalize_acceptance_criteria(
    acceptance_criteria: list[str | dict[str, Any]] | None,
) -> list[str | dict[str, str]]:
    normalized: list[str | dict[str, str]] = []
    for item in acceptance_criteria or []:
        if isinstance(item, str):
            text = item.strip()
            if text:
                normalized.append(text)
            continue
        if isinstance(item, dict):
            text = str(item.get("text") or "").strip()
            if not text:
                continue
            binding = str(item.get("verification_binding") or "").strip()
            row = {"text": text}
            if binding:
                row["verification_binding"] = binding
            normalized.append(row)
    return normalized


async def _draft_acceptance_criteria(title: str, description: str) -> list[str]:
    if not ensure_llm_env():
        raise ValueError(
            "acceptance criteria are required when no governance model is configured; "
            "pass acceptance_criteria or configure a model"
        )
    try:
        model = build_model()
        result = await structured_output_ainvoke(
            model,
            _ACList,
            [
                SystemMessage(content=_AC_DRAFT_SYSTEM_PROMPT),
                HumanMessage(content=(f"Title: {title}\n\nDescription:\n{description[:8000]}")),
            ],
        )
        return [ac.strip() for ac in result.acceptance_criteria if ac.strip()]
    except Exception as exc:
        logger.warning("AC drafting LLM failed", exc_info=True)
        raise ValueError(
            "acceptance criteria could not be drafted; pass acceptance_criteria "
            "or fix the model configuration"
        ) from exc


async def create_quick_work_item(
    title: str,
    description: str,
    issue_url: str = "",
    issue_key: str = "",
    feature_key: str = "",
    feature_name: str = "",
    created_by: str = "",
    workspace_id: str = "",
    acceptance_criteria: list[str | dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Create a quick-route CR from issue content submitted by a developer in the IDE.

    Steps:
    1. Use caller-provided acceptance criteria, or draft them via LLM from title + description.
    2. Upsert the feature only when the caller supplied feature_key.
    3. Create CR with work_type=bug_fix and the effective ACs.
    4. Return a Ready work item; Doc Registry derives its Context Pack on read.
    5. Return {change_request_id, change_request_key, feature_id,
               acceptance_criteria, phase}.
    """
    workspace_id = workspace_id.strip()
    if not workspace_id:
        raise ValueError("workspace_id is required")

    client = DocRegistryClient(doc_registry_base_url())

    acs = _normalize_acceptance_criteria(acceptance_criteria)
    if not acs:
        acs = await _draft_acceptance_criteria(title, description)
    if not acs:
        raise ValueError(
            "acceptance criteria could not be drafted; pass acceptance_criteria "
            "or fix the model configuration"
        )

    effective_key = feature_key.strip()
    effective_name = feature_name.strip() or title
    feature_id = ""
    if effective_key:
        feature = await client.aupsert_feature_by_key(
            effective_key, effective_name, workspace_id=workspace_id
        )
        feature_id = str(feature["id"])

    cr_body: dict[str, Any] = {
        "work_type": "bug_fix",
        "title": title,
        "intent_md": description,
        "acceptance_criteria_json": json.dumps(acs, ensure_ascii=False),
    }
    if feature_id:
        cr_body["feature_id"] = feature_id
    if created_by.strip():
        cr_body["created_by"] = created_by.strip()
    cr_body["workspace_id"] = workspace_id
    if issue_url:
        cr_body["source_refs_json"] = json.dumps([issue_url], ensure_ascii=False)

    cr = await client.acreate_change_request(cr_body, workspace_id=workspace_id)
    cr_id: str = cr["id"]
    cr_key: str = cr.get("key", "")

    result: dict[str, Any] = {
        "change_request_id": cr_id,
        "change_request_key": cr_key,
        "title": title,
        "acceptance_criteria": acs,
        "phase": "ready",
    }
    if feature_id:
        result["feature_id"] = feature_id
        result["feature_key"] = effective_key
    return result
