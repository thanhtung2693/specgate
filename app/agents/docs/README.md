# Agent Docs

This folder is the home for the LangGraph-based agent module.

## Canonical docs

- **[prd.md](./prd.md)** — why the agent module exists and what outcomes it owns.
- **[spec.md](./spec.md)** — module-level graph structure, governance-op
  responsibilities, state shape, and tool contracts for the governance-chat node.
- **[governance/docs/spec.md](./governance/docs/spec.md)** — governance runtime contract:
  the single governance-chat node, its tools, REST wiring, and graph boundaries.
- **[governance/docs/event-contract.md](./governance/docs/event-contract.md)** —
  governance-ops-to-UI event and hydration contract.

## Boundaries

- Governance-ops orchestration (readiness gates, delivery review, and Full-mode
  quick-work acceptance-criteria drafting) belongs here.
- The read-only governance-chat graph and health route belong here.
- Doc Registry storage internals belong in the Doc Registry module.
- React rendering details belong in the UI module, with this module owning only
  the governance-ops event/read-model contract.
