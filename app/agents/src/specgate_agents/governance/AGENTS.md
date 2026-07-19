# Governance Package Contributor Rules

Extends [`app/agents/AGENTS.md`](../../../AGENTS.md). This file applies to the
governance package implementation, not to coding agents using the SpecGate
product.

## Runtime role

The package provides a headless governance-operations layer and a thin
governance-chat surface. `governance_chat.py:graph` is the sole LangGraph graph:
one graph node built with `langchain.agents.create_agent` on the dedicated
governance-chat model and exactly the four read-only governance tools. Gate
execution, delivery review, and other state-changing operations remain
deterministic services called by `webapp.py`.

## Invariants

- The runtime governance-chat agent does not draft PRDs, specifications, or
  implementation plans; edit repositories; or create governed artifact bodies.
- The runtime graph does not spawn product subagents or introduce
  human-in-the-loop interrupts. “No subagents” here describes product runtime
  behavior, not how contributors organize coding work.
- Governance tools read governed artifacts, stored readiness, and Knowledge.
  State-changing operations require their explicit service/API path.
- Do not fabricate missing product facts. Read the governed source, run the
  appropriate operation, or expose the assumption. Cite artifact identifiers
  and gate names when they support the answer.
- Apply the parent module's structured LLM rule to all natural-language routing,
  classification, and extraction.
- The chat model is configured through
  `build_governance_ops_model`. Server-side gate/review model settings configured
  through the CLI are a separate concern; do not merge the two configurations.
- Registry access is REST-only. Do not add repository reads, database access,
  object-store access, or transport overlays.
- Narration remains in the assistant reply. Large artifact bodies travel by
  reference rather than being copied into messages, events, or traces.

Keep the implementation aligned with:

- `app/agents/docs/governance/docs/spec.md`
- `app/agents/docs/governance/docs/event-contract.md`
