# Testing Strategy

Single entry point for verifying SpecGate across all four modules. If you're an
AI agent or a teammate asked to "run the tests" or "verify", start here.

## Architecture context (what shapes the test surface)

The agents module is a **headless governance-ops service**: one LangGraph graph
`governance` → [`governance_chat.py`](../app/agents/src/specgate_agents/governance/governance_chat.py)
— a single deep-agent node exposing governance read/run tools, with **no drafting
sub-agents and no HITL interrupts**. The broader governance operations (readiness
gates, reconciliation, classification, summarization) are deterministic Python
services invoked by `webapp.py` HTTP routes, not graph nodes.

That means the test surface is **unit + harness over deterministic services**,
CLI command/client tests, UI component tests, and the Doc Registry Go suite —
there is no sub-agent streaming, trace-replay, or live-scenario layer.

## How we test

Run cheapest first; stop at the layer that proves the contract you care about.

| Layer | Cost | What it proves | How to run |
|-------|------|----------------|------------|
| **1. Unit + harness** | seconds, no LLM | Governance-op logic (gates, reconciliation, classification), CLI commands and JSON envelopes, MCP tool contracts, DTO / wire shapes, UI components, status-transition + event atomicity | `agents`: `uv run pytest` · `cli`: `make test` · `ui`: `npm run test -- --run` · `doc-registry`: `make test` |
| **2. Opt-in live smoke** | network | The LangSmith tracing boundary still accepts + reads back a run | `GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke` |
| **3. Opt-in eval** | LLM tokens | Governance judgments (quality gates, reconciliation, lifecycle) score above threshold on fixed datasets | `cd app/agents && uv run python -m evals.run --target <t> --dry-run` — see [`agents/evals/README.md`](../app/agents/evals/README.md) |

## CI enforcement

Layer 1 runs in CI via per-module GitHub Actions workflows in
[`.github/workflows/`](../.github/workflows), each path-filtered to its module so
only affected suites run:

| Workflow | Runs |
|----------|------|
| `cli.yml` | `make test` (Go, race detector) |
| `doc-registry.yml` | `go test -race` (testcontainers Postgres) |
| `agents.yml` | `ruff check` + `uv run pytest` |
| `ui.yml` | `npm ci` -> `lint` + `build` + `test` (Node 26) |
| `plugins.yml` | `make check-plugins` |

The opt-in live-smoke and eval layers (2 and 3) are **not** run in CI; they need
network/LLM credentials and stay manual.

A repo-wide **release-readiness gate** runs on every push and pull request via
[`release-readiness.yml`](../.github/workflows/release-readiness.yml):
`node --test docs/release-readiness.test.mjs`. It scans all tracked files for
retired terminology and asserts release positioning, packaging/compose defaults,
and cross-module contract docs, so terminology or packaging regressions fail the
build. `release.yml` publishes the CLI with GoReleaser, uploads the compose
bundle, and builds multi-platform `linux/amd64` + `linux/arm64` container images
on release tags.

## How we observe

| Surface | Where to look |
|---------|---------------|
| LangSmith traces | Each governance-op route (`webapp.py`) is wrapped with `_traced`, so it surfaces as a root `chain` run tagged `governance` / `agent-api` that nests the LLM/MCP work it triggers. Correlate across modules by `custom_metadata.thread_id`. Set `LANGSMITH_API_KEY` + `LANGSMITH_PROJECT` in `app/agents/.env`. |
| LangGraph thread state | `curl -s http://localhost:2024/threads/<id>/state` — `values.messages` is the transcript channel (projected to typed UI entries by `/governance/threads/<id>/transcript`); `values.thread_title` carries the title. |
| Container source vs worktree | `docker exec specgate-agents-1 python -c "import inspect, specgate_agents.governance.governance_chat as m; print(inspect.getsourcefile(m))"` confirms the running container sees your code. |

## What to test

### Governance-chat node (`governance_chat.py`)
- Binds exactly the four read/run governance tools (`get_artifact`,
  `get_artifact_documents`, `list_artifact_readiness`, `run_artifact_readiness`)
  — no drafting tools. (`tests/governance/test_governance_chat.py`)
- System prompt keeps the node in governance scope (explain gate failures, compare
  versions, surface conflicts, summarize deviations) and off artifact authoring.

### Readiness + quality gates
- `completeness` locates the minimum-executable-contract topics **by role** over any
  spec format; a missing required topic is advisory (`warn`, never `fail`).
  (`tests/governance/test_completeness.py`)
- Profile-driven `enabled_gates` / `required_roles` / `required_topics` are read from
  the artifact's snapshot. (`tests/governance/test_artifact_readiness.py`)
- `delivery_review` clamps `pass → needs_human_review` when the profile is
  `corroborated_required` and no `delivery.pr_merged` event exists for the change
  request. (`tests/governance/test_delivery_review.py`, `test_board_delivery_review.py`)
- Gate judge structured output; profile-bound Skills inject as a rubric.
  (`tests/governance/test_quality_gate_judge.py`)

### Reconciliation
- `draft_artifact_update_proposal` targets the governed envelope by path + role,
  opens a path-keyed edit session tagged `source_kind=feedback_event`, and dedups on
  `(base_artifact_id, source_kind, source_id)`. (`tests/governance/test_reconciliation_proposal.py`)

### Doc Registry contract
- Status transition + `artifact_events` row in the same transaction.
- Artifact DTO surfaces `expected_gates` derived from the snapshot.
- Evidence provenance is **server-stamped**: the `report_implementation_feedback`
  MCP handler blanks agent-supplied `source` so corroboration can't be self-claimed.
- Run with `make test` (Postgres-only via testcontainers-go).

### CLI
- Command surfaces return stable JSON envelopes in `--json` mode and keep exit
  codes aligned with `internal/output`.
- Local user/workspace selection scopes work-listing and attribution without
  becoming authentication.
- Run with `make test` from `app/cli`.

### UI
- Governance-chat rendering (reasoning part, tool-disclosure), the expected-gates
  Not-run rows in the readiness panel, the feedback inbox. (`npm run test`)
- Dead-code guard: `npx knip --include files,dependencies` reports no unused
  files or dependencies. Shared shadcn/UI primitive exports are intentionally
  outside that guard.

## Layer 1 — Unit + harness

### agents
```bash
cd app/agents
uv run pytest -q                    # no LLM by default (ScriptedChatModel)
uv run pytest tests/governance -q   # governance subset
uv run ruff check src tests      # lint (format + F-rules)
```
- The default run excludes the opt-in `live_smoke` test (1 deselected).
- `LangGraphTestHarness` (`tests/governance/_harness/`) wires a real compiled graph with
  scripted models; `FakeRegistryClient` / `InMemoryRegistry` stand in for Doc Registry.

### ui
```bash
cd app/ui
npm run test -- --run            # vitest component tests
npm run lint                     # oxlint
npm run build                    # tsc -b (strict) + vite build
npx knip --include files,dependencies
```

### doc-registry
```bash
cd app/doc-registry
make test                        # go test -race -count=1 ./...
make lint                        # go vet ./...
```

### cli
```bash
cd app/cli
make test                        # go test -race -count=1 ./...
make lint                        # go vet ./...
```

## Layer 2 — Live smoke (opt-in)

```bash
cd app/agents
GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke -q
```
- One test: `test_live_langsmith_trace_roundtrip` creates a LangSmith run and reads
  it back, proving the tracing boundary is wired. Skips unless `GOVERNANCE_LIVE_SMOKE=1`
  and a `LANGSMITH_API_KEY` / `LANGCHAIN_API_KEY` is set.

## Layer 3 — Eval (opt-in, LLM)

```bash
cd app/agents
uv run python -m evals.run --target quality_gate_contract --dry-run   # plan only, no LLM
uv run python -m evals.run --target quality_gate_contract             # real run; needs provider keys
```
- Datasets at `app/agents/evals/datasets/*.jsonl`; thresholds at `app/agents/evals/thresholds.toml`.
- Pushes results to LangSmith when `LANGSMITH_API_KEY` is set.
- See [`agents/evals/README.md`](../app/agents/evals/README.md). The current governance
  targets are `quality_gate_contract`, `ac_satisfaction_contract`, and
  `reconciliation_contract`, plus the `quality_gates`, `lifecycle`, and
  `reconciliation` gold-data suites.

## LangGraph Streaming SSE Probe (`/runs/stream`)

When a governance-chat streaming bug looks like "wait N seconds, response pops in at
once", **probe the raw `/runs/stream` wire before touching UI code** — the browser
hides whether the backend emits tokens at all and under which `stream_mode`.

### Prerequisites
- LangGraph API reachable at `http://localhost:2024` (Docker `specgate-agents-1` or
  native `uv run langgraph dev`).
- `curl`, `uuidgen`, `jq`.

### Reloading the Docker LangGraph container

The self-hosted server imports the graph once at startup and does not hot-reload
Python — `docker compose watch agents` only **syncs** the file into the container.
Confirm what's actually running before you trust a probe:

```bash
docker exec specgate-agents-1 python -c "
import inspect, specgate_agents.governance.governance_chat as m
print('source:', inspect.getsourcefile(m))
"
```

To load Python changes, `docker restart specgate-agents-1` (re-imports the synced
editable install) or use native `uv run langgraph dev`. A bare restart of a baked
`langgraph build` image ignores changes — see the container-reload note in the root
`AGENTS.md` §9.

### Probe each `stream_mode`

```bash
THREAD=$(uuidgen)
PROMPT='Explain why the spec_completeness gate failed for an artifact.'
for MODE in values messages-tuple updates; do
  echo "--- $MODE ---"
  curl -s -N -X POST "http://localhost:2024/threads/$THREAD/runs/stream" \
    -H 'Content-Type: application/json' \
    -d "{
      \"assistant_id\": \"governance\",
      \"input\": {\"messages\": [{\"type\": \"human\", \"content\": \"$PROMPT\"}]},
      \"stream_mode\": [\"$MODE\"]
    }" | head -c 4000 | grep '^data: ' | sed 's/^data: //' | jq -c -r 'select(.type)'
done
```

`messages-tuple` should emit AIMessage chunks (including governance tool calls) as
tokens stream; `values` / `updates` should emit state snapshots / partial writes. The
single node has no sub-agent namespace fan-out. Empty output on `messages-tuple`
means the backend isn't producing tokens — fix the agent before touching the UI.

## Browser walkthrough — state-API cross-check

The fastest way to localize a governance-chat rendering bug is to drive the UI in a
real browser and, after each turn, diff what the user sees against what LangGraph
thread state actually persisted.

**Per UI turn:**

1. Trigger the action (Chrome MCP or by hand); wait for the run to finish.
2. `curl -s http://localhost:2024/threads/<id>/state` and read the last few
   `values.messages` — check content shape (`string` vs block array
   `[{type:"text", text:"…"}]`).
3. Cross-check against the browser:
   - **state has it, UI does not** → UI hydration / snapshot-rebuild bug. Hard-reload
     (`cmd+shift+r`); if reload renders it, only the post-run rebuild is broken.
   - **state is wrong / missing** → backend / stream-emitter bug. Pull the LangSmith
     trace next.
   - **UI shows partial content** (cuts off mid-word / mid-marker) → live-stream text
     parser / markdown renderer bug.

**Why this beats "just look at the UI":** the LangSmith waterfall tells you the
route/run sequence, but not always the exact *shape* of persisted content — a
`content: [{type:"text"}]` block can render while streaming and vanish after the
snapshot rebuild. You catch that only by reading the raw state.

## Bug triage loop

1. **Reproduce.** Re-run the prompt in the governance-ops.
2. **Layer down.** Does a unit test catch it? If not, can you write one?
3. **Trace inspect.** LangSmith waterfall — find the bad span; check the governance-op
   route's inputs/outputs.
4. **Browser inspect.** Compare the rendered transcript with the raw LangGraph
   thread state. State right but render wrong → component bug. State wrong →
   backend / stream-emitter.
5. **Pin.** Add the unit test (agents/UI) closest to the cause.

## References

- Module specs: [`../app/agents/docs/spec.md`](../app/agents/docs/spec.md), [`../app/agents/docs/governance/docs/spec.md`](../app/agents/docs/governance/docs/spec.md)
- Event contract: [`../app/agents/docs/governance/docs/event-contract.md`](../app/agents/docs/governance/docs/event-contract.md)
- Eval harness: [`../app/agents/evals/README.md`](../app/agents/evals/README.md)
