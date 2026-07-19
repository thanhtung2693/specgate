# Governance docs index

Reading order for a coding agent picking up governance work.

## Canonical contracts

1. **[spec.md](./spec.md)** — governance runtime contract. Covers the single
   governance-chat node, its governance-op tools, state fields, REST wiring, and
   UI-facing chat behavior. No drafting sub-agents and no HITL.

2. **[event-contract.md](./event-contract.md)** — agent-to-UI event contract.
   Live LangGraph stream modes, UI surfaces, and resume/hydration shapes.

## Regression references

The governance-chat UI surface is covered by component tests in the UI module
(e.g. `../../../../ui/src/components/agent/`). Runbook-style scenario
coverage lives in [docs/contributing/testing.md](../../../../../docs/contributing/testing.md).
