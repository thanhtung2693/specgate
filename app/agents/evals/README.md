# Governance contract evals

Offline, version-controlled eval harness for the two governance contract
targets (`quality_gate_contract`, `ac_satisfaction_contract`). Datasets are
JSONL files in this directory tree, the contract judge lives in
`eval_contract.py`, and the runner is `run.py`.

## Layout

```
evals/
├── datasets/
│   ├── quality_gate_contract.jsonl
│   └── ac_satisfaction_contract.jsonl
├── fixtures/                # markdown snippets referenced by gold-data suites
├── eval_contract.py         # contract judge (FakeJudge + LiveJudge) + schemas
├── harness.py               # loader + dispatcher + scoring + push
├── _factories.py            # model + provider-key builders for the harness
├── run.py                   # CLI (uv run python -m evals.run)
├── thresholds.toml          # per-target aggregate pass-rate gates
└── README.md
```

## Running locally

```bash
# Preview the plan without spending any tokens:
cd app/agents
uv run python -m evals.run --target quality_gate_contract --dry-run

# Run one target end-to-end (deterministic FakeJudge; no provider keys):
uv run python -m evals.run --target quality_gate_contract

# Run everything; emits structured JSON on stdout for CI:
uv run python -m evals.run --target all --json

# Mirror results into LangSmith (opt-in; requires LANGSMITH_API_KEY):
LANGSMITH_API_KEY=... uv run python -m evals.run --target all --push
```

Provider keys hydrate the same way as the production graph — either via
environment variables (`OPENAI_API_KEY`, `GOOGLE_API_KEY`,
`ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`) or by seeding the Doc Registry
model settings.
When keys are missing the CLI exits with code `78` and a clear message;
this is distinct from `1` (threshold not met) and `2` (CLI usage).

## Adding a row

1. Pick the target dataset under `datasets/`.
2. Add one JSON object on its own line. Mandatory keys: `id`, `input`,
   `expected`. `input.suite` selects the contract judge branch
   (`quality_gate`, `ac_satisfaction`); `expected`
   carries `verdict` and `confidence_min`. Use the existing rows as a
   template.
3. Run the harness against the target locally and confirm the new row
   passes (or fails for an intended reason).

## How scoring works

The harness runs each row through the contract judge in `eval_contract.py`
and collects the structured `(verdict, confidence, diagnostics)` result.
The default `--contract-judge fake` mode is a deterministic baseline;
`--contract-judge live` calls the configured model. Each row scores against
two assertions:

| Target | Scorers |
|---|---|
| `quality_gate_contract` | `verdict` match, `confidence_at_least(confidence)` via contract judge |
| `ac_satisfaction_contract` | `verdict` match, `confidence_at_least(confidence)` via contract judge |

A row passes when every applicable scorer passes. A target meets its
threshold when the aggregate pass-rate meets the value in
`thresholds.toml`.

## Contract targets (offline, no provider keys)

The contract targets are deterministic and run without LLM provider keys:

- `quality_gate_contract` → `datasets/quality_gate_contract.jsonl`
- `ac_satisfaction_contract` → `datasets/ac_satisfaction_contract.jsonl`

Example:

```bash
cd app/agents
uv run python -m evals.run --target quality_gate_contract --json
```

Live calibration mode (uses configured model/provider keys):

```bash
uv run python -m evals.run --target quality_gate_contract --contract-judge live --json
```

## Gold-data suites (gate judging)

These gold-data suites live in subpackages and run as plain `pytest` modules
(not through the JSONL CLI). Each defines a `GOLD_FIXTURES` list, a
`score_*` function returning `(score, diagnostics)`, deterministic fake-judge
tests for CI, and one opt-in `live_smoke` calibration test.

- `evals/quality_gates/` — the five LLM quality gates judged by
  `specgate_agents.governance.quality_gates.judge` (`rollback_plan_present`,
  `acceptance_criteria_edge_cases`, `success_metric_measurable`, `scope_clear`,
  `implementation_plan_traceable`). Per-gate fixtures pair a compact artifact
  body with the resolved `GateState` a faithful judge should reach; the
  expected state is derived from `resolve_gate_state` so the offline path
  exercises the low-confidence-pass → `needs_human_review` downgrade rather
  than a tautology. Each gate has at least one clear pass and one clear
  fail/warn fixture.

```bash
cd app/agents
# Deterministic CI path (no provider keys):
uv run pytest evals/quality_gates

# Live calibration (real judge; keys hydrate from Doc Registry settings):
GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke --override-ini="addopts="
```

The gate suite's live calibration asserts the aggregate gold score is `>= 0.75`.

## Launch gate policy

Before enabling any gate-judged product surface, pass these checks:

| Surface being enabled | Required eval command(s) | Pass rule |
|---|---|---|
| Contract schema or scorer changes | `uv run python -m evals.run --target quality_gate_contract --contract-judge fake --json`<br/>`uv run python -m evals.run --target ac_satisfaction_contract --contract-judge fake --json` | Both targets must meet threshold (`threshold_met=true`) |
| LLM calibration change (model/prompt for contract judge) | Same commands with `--contract-judge live` | No target may regress below threshold |

Release decision rule:

- Do not ship the related surface when any required target is below threshold.
- Threshold edits in `thresholds.toml` must be reviewed in the same PR as rationale.

## Interpreting output

Stderr carries the human-readable summary; stdout carries the
structured JSON when `--json` is set:

```
[PASS] quality_gate_contract: 2/2 passed (100%, threshold 100%)
  [ok] qg-contract-001 (1ms)
  [ok] qg-contract-002 (0ms)
  ...
```

A `[FAIL]` row prints its failed scorer name + the reason string. An
`[ok]` row passed every applicable scorer.

## LangSmith integration

The push is additive, never required. Set `LANGSMITH_API_KEY` and pass
`--push`; the harness mirrors each per-row verdict into a LangSmith
dataset named `governance-<target>` under your default project. Missing
datasets are created on demand. The harness tolerates a missing
`langsmith` install or a network failure — the local run still
completes.

## Cost estimate

The contract targets default to the deterministic `FakeJudge`, so a
`--target all` run costs nothing. `--contract-judge live` calls the
configured main model once per row (six rows total), staying well under a
cent at current pricing.

## Datasets are code

Adding a row is a PR. Editing a row is a PR. No hidden state in the
LangSmith UI. The harness exists so any contributor can re-run the same
eval bit-for-bit from a fresh clone.
