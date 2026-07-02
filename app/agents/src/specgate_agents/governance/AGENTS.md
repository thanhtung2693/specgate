# SpecGate Governance Memory

## Role

The governance service is SpecGate's **headless governance-ops layer plus a thin
governance-chat surface**. It does not draft PRDs, specs, or implementation
plans — creation happens in the developer's IDE. It reads governed artifacts,
runs readiness/quality gates, reconciles implementation feedback into update
proposals, classifies, and summarizes.

The only LangGraph graph is `governance` → `governance_chat.py:graph`: a single
deep-agent node on the dedicated governance-chat model, exposing governance read/run operations
as tools. The broader governance operations (delivery review, reconciliation,
route classification, context-pack compilation, lifecycle, summarization) are
deterministic Python services invoked directly by `webapp.py` FastAPI handlers —
they do not route through the graph.

## Always-on invariants

- **No drafting, no sub-agents, no HITL.** The chat node answers and runs
  governance-ops; it does not generate artifact bodies, spawn `prd`/`impl`/
  `research`/`context` sub-agents, or raise review/clarify interrupts. Do not
  reintroduce a drafting topology from `build_supervisor`'s broader signature —
  governance-chat supplies only `model`, `system_prompt`, `tools`, `name`.
- **Creation is IDE-side.** Point users at their IDE for authoring; SpecGate
  governs (register → version → review → approve → canonical → hand off →
  reconcile), it does not compete with IDE agents for open-ended repo work.
- **Do not fabricate missing product facts.** Read the artifact, run the gate,
  or state the assumption — never invent. Cite artifact ids and gate names.
- **LLM-driven control flow only.** No keyword routing, rule-based
  classification, or heuristics over user content. Route intent / classification
  / extraction through `llm_structured.py::structured_output_ainvoke` with
  structured output (see `quality_gates/judge.py`). Structural code keys (event
  names, status enums, MCP tool names, JSON schema keys) may be matched; user
  text may not.
- **Dedicated chat model, no overlays.** The node runs on the dedicated
  governance-chat support model (`agents/factories.py::build_governance_ops_model`),
  selected by `GOVERNANCE_OPS_MODEL_PROVIDER` / `GOVERNANCE_OPS_MODEL` /
  `GOVERNANCE_OPS_API_KEY` / `GOVERNANCE_OPS_THINKING_LEVEL` env vars.
  `specgate model set` configures the separate server-side model used by gates,
  classifiers, readiness, delivery review, and summaries.
- **MCP is narrowed to Doc Registry reads.** No outbound repo-read MCP (repo
  reading is IDE-side). Skills are a Doc Registry registry surfaced to IDEs via
  the `specgate://skills` MCP resource; readiness gates use fixed prompts.
- **Narration is inline.** The assistant narrates in its own reply; there is no
  separate companion channel. Large bodies travel by reference, never inline in
  messages or transcripts.

See `docs/governance/docs/spec.md` for the node contract and
`docs/governance/docs/event-contract.md` for the UI-facing wire shapes.
