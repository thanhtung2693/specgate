# Testing strategy

Use this reference when you need to choose the right verification for a
SpecGate change. Run the cheapest check that can fail for the behavior you
touched, then broaden only when the change crosses module or contract
boundaries.

## Test layers

| Layer | Use for | Command |
|---|---|---|
| Module unit and harness tests | Normal code changes, CLI output, Go handlers, UI components, governance-op logic | See module commands below |
| Release readiness gate | Packaging, public docs, release images, terminology, static release contracts | `node --test docs/release-readiness.test.mjs` |
| Browser or API walkthrough | UI behavior, onboarding, Context Pack handoff, delivery review flows | Manual or Playwright, plus API readback |
| Live smoke | LangSmith tracing boundary | `GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke -q` |
| Eval run | LLM-backed gate quality on fixed datasets | `uv run python -m evals.run --target <target>` |

Layer 1 runs in CI. Live smoke and evals are opt-in because they need network
access, provider credentials, or LangSmith credentials.

## Module commands

### CLI

```bash
cd app/cli
make test
make lint
```

The CLI suite covers command behavior, JSON envelopes, exit codes, local
configuration, user/workspace selection, plugin installation, and uninstall
cleanup.

### Doc Registry

```bash
cd app/doc-registry
make test
make lint
```

The Doc Registry suite covers API handlers, database behavior, artifact state,
events, evidence, settings, and contract fixtures. Tests use testcontainers for
Postgres where needed.

### Governance-ops

```bash
cd app/agents
uv run pytest -q
uv run pytest tests/governance -q
uv run ruff check src tests
```

Default tests use scripted models and do not require LLM keys. The governance
chat graph is a single governance-ops node with read/run tools; readiness gates,
delivery review, summaries, and classification run through deterministic Python
services and HTTP routes.

### UI

```bash
cd app/ui
npm run test -- --run
npm run lint
npm run build
npx knip --include files,dependencies
```

Use browser verification when layout, routing, streaming, onboarding, settings,
or artifact/workflow readback changes.

## Release readiness

Run the release gate after changes to public docs, installers, Compose files,
Dockerfiles, workflows, release metadata, or cross-module contracts:

```bash
node --test docs/release-readiness.test.mjs
```

The gate checks alpha positioning, installer paths, Compose defaults, image
runtime settings, Node workflow versions, static landing metadata, terminology,
and contract references.

## What to verify by change type

| Change | Minimum useful proof |
|---|---|
| CLI command behavior | Targeted CLI tests, then `make test` in `app/cli` |
| Plugin installer or uninstall cleanup | CLI tests plus a scratch HOME run when behavior touches real files |
| Docker or Compose release files | `docker compose config --quiet`, release-readiness gate, affected CLI deploy tests |
| Doc Registry API or schema | Targeted Go tests, then `make test` in `app/doc-registry` |
| Governance-ops logic | Targeted `app/agents` pytest file, then governance subset |
| UI behavior | Targeted Vitest file, browser/API readback for rendered behavior |
| Cross-module contract | Affected module tests plus release-readiness gate |
| Docs only | Release-readiness gate when release-facing docs are touched; otherwise link and command sanity |

## Browser and API readback

When a UI bug may be caused by backend state, verify both sides:

1. Trigger the UI action.
2. Read the relevant API or LangGraph state.
3. Compare rendered content with stored content.

Useful endpoints:

```bash
curl -s http://localhost:2024/threads/<thread-id>/state
curl -s http://localhost:8080/api/v1/status
curl -s http://localhost:8080/api/v1/work-items
```

If state is correct and the browser is wrong, test the UI path. If state is
wrong, test the backend or governance-ops path.

## LangGraph streaming probe

Use this only when a governance-chat streaming bug looks like the browser waits
and then renders the response all at once.

Prerequisites:

- LangGraph API reachable at `http://localhost:2024`;
- `curl`, `uuidgen`, and `jq`.

Probe raw stream modes:

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

`messages-tuple` should emit message chunks. `values` and `updates` should emit
state snapshots or partial writes. Empty `messages-tuple` output points to the
backend stream path, not the browser renderer.

## Docker reload rule for agents

The self-hosted `specgate-agents-1` server imports Python modules once at
startup. `docker compose watch agents` syncs files into the container but does
not reload the running process.

After Python changes:

```bash
docker restart specgate-agents-1
docker inspect --format '{{.State.Health.Status}}' specgate-agents-1
```

Do not treat `inspect.getsource` from a fresh `docker exec python -c ...`
process as proof that the running server reloaded. Exercise the live API path or
restart first.

## Live smoke

```bash
cd app/agents
GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke -q
```

This checks the LangSmith trace round trip. It skips unless
`GOVERNANCE_LIVE_SMOKE=1` and `LANGSMITH_API_KEY` or `LANGCHAIN_API_KEY` is set.

## Evals

```bash
cd app/agents
uv run python -m evals.run --target quality_gate_contract --dry-run
uv run python -m evals.run --target quality_gate_contract
```

Datasets live under `app/agents/evals/datasets/`. Thresholds live in
`app/agents/evals/thresholds.toml`. Use `--dry-run` before spending model
tokens.

## Debugging loop

1. Reproduce the failure.
2. Identify the owning layer: CLI, UI, Doc Registry, governance-ops, Compose, or
   release packaging.
3. Add or update the smallest test that fails.
4. Fix the behavior.
5. Re-run the targeted test.
6. Run the broader module or release gate when the change crosses a contract.

## Related

- [Contracts](contracts.md)
- [Governance reference](reference/governance.md)
- [Operate SpecGate](guides/operate-specgate.md)
- [Release checklist](internals/oss-release-checklist.md)
