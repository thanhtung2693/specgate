"""Headless governance-chat node — a single deep-agent that answers questions
over governed artifacts and invokes governance-ops as tools. No drafting."""

from __future__ import annotations

from typing import Any

from langchain_core.tools import BaseTool, tool

from specgate_agents.governance.agents.factories import build_governance_ops_model
from specgate_agents.governance.board.quality_gates import run_llm_gates_for_artifact
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.main_agent import build_supervisor
from specgate_agents.governance.registry.client import DocRegistryClient

GOVERNANCE_CHAT_SYSTEM = """\
You are SpecGate's governance assistant. You answer questions about governed \
product artifacts and their delivery readiness, and you run governance \
operations on request. You do NOT draft PRDs, specs, or implementation plans — \
creation happens in the developer's IDE. Use your tools to read artifacts, \
explain why a readiness gate failed, compare versions, surface conflicts, and \
summarize implementation deviations. Cite artifact ids and gate names. Be concise.

When a message references an artifact with the directive form \
`:artifact[Title]{name=<id>}`, treat `<id>` as the artifact_id and load it with \
get_artifact (and list_artifact_readiness when the question is about gates) \
before answering."""


def _client() -> DocRegistryClient:
    return DocRegistryClient(doc_registry_base_url())


@tool
async def get_artifact(artifact_id: str) -> dict[str, Any]:
    """Return the governed artifact envelope (status, version, role-tagged files) by id."""
    return await _client().aget_artifact(artifact_id)


@tool
async def get_artifact_documents(artifact_id: str) -> dict[str, str]:
    """Return the artifact's documents as a path->markdown map for reading/comparison."""
    return await _client().aload_artifact_markdown_bundle(artifact_id)


@tool
def list_artifact_readiness(artifact_id: str) -> list[dict[str, Any]]:
    """List the stored readiness/quality-gate runs for an artifact (to explain failures)."""
    return _client().list_artifact_readiness_runs(artifact_id)


@tool
async def run_artifact_readiness(artifact_id: str) -> dict[str, Any]:
    """Run the profile-scoped readiness gates for an artifact and return the verdicts."""
    return await run_llm_gates_for_artifact(artifact_id)


GOVERNANCE_TOOLS: list[BaseTool] = [
    get_artifact,
    get_artifact_documents,
    list_artifact_readiness,
    run_artifact_readiness,
]


def governance_tool_names() -> set[str]:
    """Names of the governance-op tools bound to the chat node (no drafting tools)."""
    return {t.name for t in GOVERNANCE_TOOLS}


def build_governance_chat_graph() -> Any:
    """Compile the single-node governance-chat agent for langgraph.json discovery."""
    return build_supervisor(
        model=build_governance_ops_model(),
        system_prompt=GOVERNANCE_CHAT_SYSTEM,
        tools=GOVERNANCE_TOOLS,
        name="governance",
    )


def graph() -> Any:
    """Entry point for langgraph.json (`governance`)."""
    return build_governance_chat_graph()


__all__ = ["build_governance_chat_graph", "graph", "governance_tool_names", "GOVERNANCE_TOOLS"]
