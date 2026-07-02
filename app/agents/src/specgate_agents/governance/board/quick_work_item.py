"""Quick-route CR creation from IDE issue content (roadmap §P2).

Accepts tracker issue content the developer already has (title, description,
optional issue URL/key, optional feature key) and creates a governed quick-route
CR — without SpecGate reaching into the tracker directly.
"""

from __future__ import annotations

import json
import logging
import re
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
- If the description is sparse, infer reasonable criteria from the title.
- Do not include meta-criteria like "write a test for X" — phrase as verifiable behavior.
"""


class _ACList(BaseModel):
    acceptance_criteria: list[str]


def _derive_feature_key(title: str, issue_key: str) -> str:
    """Derive a stable, URL-safe feature key for explicit feature-backed work."""
    if issue_key:
        return re.sub(r"[^a-z0-9-]", "-", issue_key.lower()).strip("-")[:60]
    slug = re.sub(r"[^a-z0-9]+", "-", title.lower()).strip("-")[:40]
    return slug or "quick-bug"


async def _draft_acceptance_criteria(title: str, description: str) -> list[str]:
    if not ensure_llm_env():
        return [f"The issue described in '{title}' is resolved and verified."]
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
    except Exception:
        logger.warning("AC drafting LLM failed; using fallback criterion", exc_info=True)
        return [f"The issue described in '{title}' is resolved and verified."]


async def create_quick_work_item(
    title: str,
    description: str,
    issue_url: str = "",
    issue_key: str = "",
    feature_key: str = "",
    feature_name: str = "",
    created_by: str = "",
    workspace_id: str = "",
    acceptance_criteria: list[str] | None = None,
) -> dict[str, Any]:
    """Create a quick-route CR from issue content submitted by a developer in the IDE.

    Steps:
    1. Draft acceptance criteria via LLM from title + description.
    2. Upsert the feature only when the caller supplied feature_key.
    3. Create CR with work_type=bug_fix and the drafted ACs.
    4. Generate a quick context pack (quick_mode=True) so the delivery_pack gate passes.
    5. Return {change_request_id, change_request_key, feature_id, context_pack_uri,
               acceptance_criteria, phase}.
    """
    client = DocRegistryClient(doc_registry_base_url())

    acs = [ac.strip() for ac in (acceptance_criteria or []) if ac.strip()]
    if not acs:
        acs = await _draft_acceptance_criteria(title, description)

    effective_key = feature_key.strip()
    effective_name = feature_name.strip() or title
    feature_id = ""
    if effective_key:
        feature = await client.aupsert_feature_by_key(effective_key, effective_name)
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
    if workspace_id.strip():
        cr_body["workspace_id"] = workspace_id.strip()
    if issue_url:
        cr_body["source_refs_json"] = json.dumps([issue_url], ensure_ascii=False)

    cr = await client.acreate_change_request(cr_body)
    cr_id: str = cr["id"]
    cr_key: str = cr.get("key", "")

    # Generate the quick context pack so the delivery_pack gate passes and the
    # IDE can read the pack via the MCP context-pack resource.
    import asyncio  # noqa: PLC0415  (local import avoids circular at module level)

    from specgate_agents.governance.board.context_pack import generate_context_pack  # noqa: PLC0415

    pack_artifact_id: str = ""
    try:
        pack_result = await asyncio.to_thread(
            generate_context_pack,
            cr_id,
            quick_mode=True,
            source_evidence=f"quick-work-item: {issue_url or issue_key or title}",
        )
        artifact = pack_result.get("artifact") or {}
        pack_artifact_id = artifact.get("id", "") if isinstance(artifact, dict) else ""
    except Exception:
        logger.warning("Quick context pack generation failed for CR %s", cr_id, exc_info=True)

    context_pack_uri = f"specgate://context-pack/{cr_id}"
    phase = "handoff" if pack_artifact_id else "intake"

    result: dict[str, Any] = {
        "change_request_id": cr_id,
        "change_request_key": cr_key,
        "title": title,
        "context_pack_uri": context_pack_uri,
        "acceptance_criteria": acs,
        "phase": phase,
    }
    if feature_id:
        result["feature_id"] = feature_id
        result["feature_key"] = effective_key
    return result
