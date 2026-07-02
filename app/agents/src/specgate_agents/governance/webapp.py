"""Custom HTTP routes mounted alongside the LangGraph platform server.

Wired via ``langgraph.json`` (``http.app``). See agents/docs/spec.md §5 for the
governance-ops surface these endpoints expose.
"""

from __future__ import annotations

import asyncio
import logging
import os
from contextlib import asynccontextmanager
from typing import Any

# Pre-warm: jsonschema_specifications.__init__ runs iterdir() at module-load time.
# Importing here (before the ASGI event loop starts) ensures the scan runs
# synchronously at startup so blockbuster never sees it during request handling.
import jsonschema  # noqa: F401
from fastapi import Depends, FastAPI, HTTPException
from langgraph_sdk import get_client
from langsmith import traceable
from pydantic import BaseModel, Field

from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
from specgate_agents.governance.board.context_pack import generate_context_pack
from specgate_agents.governance.board.delivery_review import review_change_request_delivery
from specgate_agents.governance.board.quality_gates import (
    run_llm_gates_for_artifact,
    run_llm_gates_for_change_request,
)
from specgate_agents.governance.board.quick_work_item import create_quick_work_item
from specgate_agents.governance.board.route_suggestion import classify_route_for_change_request
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.provider_keys import (
    governance_model_provider,
    provider_has_api_key,
    set_provider_api_keys_from_settings,
)
from specgate_agents.governance.registry.client import DocRegistryClient
from specgate_agents.governance.title_api import (
    GenerateThreadTitleRequest,
    GenerateThreadTitleResponse,
    generate_thread_title_for_thread,
)

logger = logging.getLogger(__name__)


async def _default_thread_values_provider(thread_id: str) -> dict[str, Any] | None:
    """Production state provider: load thread state via in-process SDK loopback."""
    client = get_client()
    try:
        state = await client.threads.get_state(thread_id)
    except Exception as exc:
        logger.warning("governance state: get_state failed for %s: %s", thread_id, exc)
        return None
    if not isinstance(state, dict):
        return None
    values = state.get("values")
    return values if isinstance(values, dict) else {}


async def get_thread_values_provider():
    """FastAPI dependency seam — overridden in tests."""
    return _default_thread_values_provider


async def _hydrate_provider_keys_on_startup() -> None:
    if provider_has_api_key(governance_model_provider()):
        return
    base_url = doc_registry_base_url()
    if not base_url:
        return
    try:
        settings = await DocRegistryClient(base_url).aget_settings_unmasked_for_governance()
    except Exception as exc:
        logger.warning("webapp startup: provider hydration failed for %s: %s", base_url, exc)
        return
    set_provider_api_keys_from_settings(settings)


@asynccontextmanager
async def _governance_lifespan(_app: FastAPI):
    await _hydrate_provider_keys_on_startup()
    # Warm up langchain provider import once so the lazy importlib.import_module
    # (which triggers ScandirIterator filesystem I/O) happens in a thread at startup
    # rather than blocking the event loop on the first request.
    if ensure_llm_env():
        try:
            await asyncio.to_thread(build_model)
        except Exception:
            logger.warning("webapp startup: model warmup failed", exc_info=True)
    yield


app = FastAPI(title="specgate-governance-custom-routes", lifespan=_governance_lifespan)


def _traced(fn):
    """Make a custom agent-API route a LangSmith trace.

    Each call becomes a root ``chain`` run that nests the LLM / agent / MCP work
    the route triggers, with the route's args captured as run inputs.
    langsmith's ``traceable`` preserves the wrapped signature, so FastAPI
    dependency injection still works.
    """
    return traceable(run_type="chain", tags=["governance", "agent-api"])(fn)


@app.get("/governance/chat/health")
async def chat_health() -> dict[str, Any]:
    """Report whether the governance chat model is configured.

    Consumed by the web UI to show a capability placeholder with add-key
    instructions instead of a dead composer. Never returns the key itself.
    """
    provider = os.environ.get("GOVERNANCE_OPS_MODEL_PROVIDER", "openai").strip() or "openai"
    model_id = os.environ.get("GOVERNANCE_OPS_MODEL", "gpt-5.4-mini").strip() or "gpt-5.4-mini"
    configured = bool(os.environ.get("GOVERNANCE_OPS_API_KEY", "").strip())
    return {"configured": configured, "provider": provider, "model": model_id}


@app.post("/governance/threads/{thread_id}/title")
@_traced
async def post_thread_title(
    thread_id: str,
    body: GenerateThreadTitleRequest,
    provider=Depends(get_thread_values_provider),  # noqa: B008
) -> GenerateThreadTitleResponse:
    try:
        return await generate_thread_title_for_thread(
            thread_id,
            body,
            values_provider=provider,
        )
    except ValueError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc


class CreateQuickWorkItemBody(BaseModel):
    """Input for POST /workboard/quick-work-item.

    Issue content the developer submits from the IDE — obtained from their
    existing IDE ↔ tracker connection. SpecGate does not read the tracker
    directly; the developer passes the content here.
    """

    title: str
    description: str
    issue_url: str = ""
    issue_key: str = ""
    feature_key: str = ""
    feature_name: str = ""
    created_by: str = ""
    workspace_id: str = ""
    acceptance_criteria: list[str] = Field(default_factory=list)


class GenerateContextPackBody(BaseModel):
    """Optional body for the context-pack endpoint.

    Defaults preserve the full flow: omitting the body (or sending ``{}``) runs
    the normal PRD/spec handoff. ``quick_mode=True`` is the human-confirmed quick
    lane — an approved Context Pack without the full artifact bundle.
    """

    quick_mode: bool = False
    source_evidence: str = ""


@app.post("/workboard/change-requests/{change_request_id}/classify-route")
@_traced
async def classify_agent_route(change_request_id: str) -> dict[str, Any]:
    """LLM-suggested handoff route (quick | full) for a work item — data only.

    Reads the change request + its feature, runs the route classifier, and
    returns ``{route, confidence, rationale}``. It never commits anything: the
    human confirms or overrides the route on the work item (per spec §FR-6.1).
    """
    return await classify_route_for_change_request(change_request_id)


@app.post("/workboard/change-requests/{change_request_id}/context-pack")
@_traced
async def generate_agent_context_pack(
    change_request_id: str,
    body: GenerateContextPackBody | None = None,
) -> dict[str, Any]:
    # generate_context_pack uses a sync httpx client; offload it so we do not
    # block the ASGI event loop (LangGraph dev's blockbuster middleware will
    # otherwise raise BlockingError).
    params = body or GenerateContextPackBody()
    return await asyncio.to_thread(
        generate_context_pack,
        change_request_id,
        quick_mode=params.quick_mode,
        source_evidence=params.source_evidence,
    )


@app.post("/workboard/change-requests/{change_request_id}/run-llm-gates")
@_traced
async def run_agent_llm_gates(change_request_id: str) -> dict[str, Any]:
    """Run all LLM quality gates against the CR's lead artifact and post verdicts."""
    return await run_llm_gates_for_change_request(change_request_id)


@app.post("/artifacts/{artifact_id}/run-readiness")
@_traced
async def run_artifact_readiness(artifact_id: str) -> dict[str, Any]:
    """Run artifact-scoped readiness gates using the artifact's snapshotted profile."""
    return await run_llm_gates_for_artifact(artifact_id)


@app.post("/workboard/change-requests/{change_request_id}/review-delivery")
@_traced
async def review_agent_delivery(change_request_id: str) -> dict[str, Any]:
    """Judge the latest coding-agent completion against the CR's acceptance criteria.

    Persists the verdict as a ``delivery_review`` GateRun (the Reviewer box's
    after-the-agent-builds step). Returns ``verdict=null`` when there is no
    completion report yet or the LLM env is unconfigured.
    """
    return await review_change_request_delivery(change_request_id)


@app.post("/workboard/quick-work-item")
@_traced
async def create_quick_work_item_handler(body: CreateQuickWorkItemBody) -> dict[str, Any]:
    """Create a quick-route CR from issue content submitted by a developer in the IDE.

    Drafts ACs via LLM, upserts the feature by key, creates the CR, and
    generates the quick context pack. Returns {change_request_id, context_pack_uri,
    acceptance_criteria, phase}.
    """
    return await create_quick_work_item(
        title=body.title,
        description=body.description,
        issue_url=body.issue_url,
        issue_key=body.issue_key,
        feature_key=body.feature_key,
        feature_name=body.feature_name,
        created_by=body.created_by,
        workspace_id=body.workspace_id,
        acceptance_criteria=body.acceptance_criteria,
    )
