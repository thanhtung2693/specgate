# Governance Operations Contributor Rules

Extends the root [contributor rules](../../AGENTS.md). This file applies only to
changes under `app/agents/`.

## Read before changing behavior

- [`docs/spec.md`](docs/spec.md): module contract
- [`docs/governance/docs/spec.md`](docs/governance/docs/spec.md): governance
  chat graph contract
- [`docs/governance/docs/event-contract.md`](docs/governance/docs/event-contract.md):
  UI-facing event shapes
- [`src/specgate_agents/governance/AGENTS.md`](src/specgate_agents/governance/AGENTS.md):
  package invariants when editing that subtree

Run the service with `uv sync --all-groups` and `uv run langgraph dev`; see
[`README.md`](README.md) for the complete contributor flow.

## Architecture boundaries

- `governance_chat.py:graph` is the only graph declared in `langgraph.json`. It
  is a thin, read-only governance-chat surface over governed artifacts,
  readiness results, and Knowledge.
- State-changing governance operations such as gate execution and delivery
  review are deterministic Python services exposed by `webapp.py`; do not route
  them through chat merely for convenience.
- Repository implementation and artifact authoring stay in IDE/CLI workflows.
  Do not add open-ended repository tools, PRD/spec drafting, or transport
  overlays to governance chat.
- Access Doc Registry through its documented REST API. Do not couple this
  module to Postgres, SQLite, S3, or Doc Registry internals.
- Add a `DocRegistryClient` method only when production code calls it.
- Design references are governed artifact metadata; this service does not
  inspect external design tools directly.

## Natural-language control flow

SpecGate accepts English, Vietnamese, mixed-language input, abbreviations, and
domain-specific phrasing. Do not route, classify, or extract meaning from user
text with keyword lists, regexes, fixed phrases, punctuation, capitalization,
length thresholds, or language detection.

Use LLM classification with structured output through
`llm_structured.py:structured_output_ainvoke`. Structural program values such
as event names, enum values, node IDs, environment names, and JSON keys may be
matched directly.

## Code and tests

- Python 3.12+, `uv`, typed Python, `ruff`, and `pytest`.
- Secrets come from environment or encrypted server settings. Never place them
  in graph state, tool output, prompts, or traces.
- Keep prompts and structured-output enums in code; keep their behavioral
  contract in the nearest spec.
- Default tests must use scripted models and require no provider credentials.
- `live_smoke` is opt-in:
  `GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke`.

Run targeted tests first, then for shared behavior:

```bash
uv run pytest -q
uv run ruff check src tests
uv run ruff format --check src tests
```

For streaming changes, use the live SSE probe and reload rules in
[`docs/contributing/testing.md`](../../docs/contributing/testing.md#langgraph-streaming-probe).

## Documentation

- Intent changes update `docs/prd.md`.
- Module behavior updates `docs/spec.md`.
- Governance-chat graph or tool changes update
  `docs/governance/docs/spec.md` and, when applicable,
  `docs/governance/docs/event-contract.md`.
- Cross-module shapes update `/docs/contributing/contracts.md`.
