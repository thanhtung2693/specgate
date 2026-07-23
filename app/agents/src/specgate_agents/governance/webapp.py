"""Custom HTTP routes mounted alongside the LangGraph platform server.

Wired via ``langgraph.json`` (``http.app``). See agents/docs/spec.md §5 for the
governance-ops surface these endpoints expose.
"""

import asyncio
import logging
import os
from contextlib import asynccontextmanager
from typing import Any

# Pre-warm: jsonschema_specifications.__init__ runs iterdir() at module-load time.
# Importing here (before the ASGI event loop starts) ensures the scan runs
# synchronously at startup so blockbuster never sees it during request handling.
import jsonschema  # noqa: F401
from fastapi import FastAPI, HTTPException, Query
from langsmith import traceable
from pydantic import BaseModel, Field

from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
from specgate_agents.governance.board.delivery_review import review_change_request_delivery
from specgate_agents.governance.board.quality_gates import (
    run_llm_gates_for_artifact,
    run_llm_gates_for_change_request,
)
from specgate_agents.governance.board.quick_work_item import create_quick_work_item
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.provider_keys import (
    governance_model_provider,
    provider_has_api_key,
    set_provider_api_keys_from_settings,
)
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)


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

    Each call becomes a root ``chain`` run that nests the LLM / agent work
    the route triggers, with the route's args captured as run inputs.
    langsmith's ``traceable`` preserves the wrapped signature, so FastAPI
    dependency injection still works.
    """
    return traceable(run_type="chain", tags=["governance", "agent-api"])(fn)


def _route_failure(operation: str, exc: Exception) -> HTTPException:
    logger.warning("%s failed (%s)", operation, type(exc).__name__, exc_info=True)
    return HTTPException(status_code=502, detail=f"{operation} failed")


@app.get("/governance/chat/health")
async def chat_health() -> dict[str, Any]:
    """Report whether the governance chat model is configured.

    The web UI hides chat unless this route is reachable and reports configured.
    Never returns the key itself.
    """
    provider = os.environ.get("GOVERNANCE_OPS_MODEL_PROVIDER", "openai").strip() or "openai"
    model_id = os.environ.get("GOVERNANCE_OPS_MODEL", "gpt-5.4-mini").strip() or "gpt-5.4-mini"
    configured = bool(os.environ.get("GOVERNANCE_OPS_API_KEY", "").strip())
    return {"configured": configured, "provider": provider, "model": model_id}


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
    workspace_id: str = Field(min_length=1)
    acceptance_criteria: list[str | dict[str, Any]] = Field(default_factory=list)


@app.post("/workboard/change-requests/{change_request_id}/run-llm-gates")
@_traced
async def run_agent_llm_gates(
    change_request_id: str, workspace_id: str = Query(..., min_length=1)
) -> dict[str, Any]:
    """Run all model-judged quality gates against the CR lead artifact and post verdicts."""
    try:
        return await run_llm_gates_for_change_request(change_request_id, workspace_id=workspace_id)
    except Exception as exc:
        raise _route_failure("run_llm_gates", exc) from None


@app.post("/artifacts/{artifact_id}/run-readiness")
@_traced
async def run_artifact_readiness(
    artifact_id: str, workspace_id: str = Query(..., min_length=1)
) -> dict[str, Any]:
    """Run artifact-scoped readiness gates using the artifact's snapshotted profile."""
    try:
        return await run_llm_gates_for_artifact(artifact_id, workspace_id=workspace_id)
    except Exception as exc:
        raise _route_failure("run_artifact_readiness", exc) from None


@app.post("/workboard/change-requests/{change_request_id}/review-delivery")
@_traced
async def review_agent_delivery(
    change_request_id: str, workspace_id: str = Query(..., min_length=1)
) -> dict[str, Any]:
    """Judge the latest coding-agent completion against the CR's acceptance criteria.

    Persists the verdict as a ``delivery_review`` GateRun (the Reviewer box's
    after-the-agent-builds step). Returns ``verdict=null`` when there is no
    completion report yet or the LLM env is unconfigured.
    """
    try:
        return await review_change_request_delivery(change_request_id, workspace_id=workspace_id)
    except Exception as exc:
        raise _route_failure("review_delivery", exc) from None


@app.post("/workboard/quick-work-item")
@_traced
async def create_quick_work_item_handler(body: CreateQuickWorkItemBody) -> dict[str, Any]:
    """Create a quick-route CR from issue content submitted by a developer in the IDE.

    Uses supplied ACs or drafts them via LLM, upserts the feature by key, creates
    the CR, and returns a Ready item. Doc Registry derives Context
    Packs on read. Returns {change_request_id, acceptance_criteria, phase}.
    """
    try:
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
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
