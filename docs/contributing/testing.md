# Testing strategy

This reference is for contributors choosing verification for a SpecGate
change. Run the cheapest check that can fail for the behavior you touched, then
broaden only when the change crosses module or contract boundaries.

## Test layers

| Layer | Use for | Command |
|---|---|---|
| Module unit and harness tests | Normal code changes, CLI output, Go handlers, UI components, governance-op logic | See module commands below |
| Release readiness gate | Packaging, public docs, release images, terminology, static release contracts | `node --test docs/release-readiness.test.mjs` |
| Browser or API walkthrough | UI behavior, onboarding, Context Pack handoff, delivery review flows | Manual or Playwright, plus API readback |
| Live smoke | LangSmith tracing boundary | `GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke -q` |
| Eval run | Model-backed gate quality on fixed datasets | `uv run python -m evals.run --target <target>` |

Layer 1 runs in CI. Live smoke and evals are opt-in because they need network
access, provider credentials, or LangSmith credentials.

## Module commands

### CLI

```bash
cd app/cli
make test
make lint
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
```

The CLI suite covers command behavior, JSON envelopes, exit codes, local
configuration, user/workspace selection, plugin installation, and uninstall
cleanup.

With a local stack running, the CLI also has opt-in new-user e2e smokes:

```bash
SPECGATE_SERVER=http://localhost:8080 bash app/cli/test/e2e/handoff.sh
SPECGATE_SERVER=http://localhost:8080 bash app/cli/test/e2e/artifact-readiness.sh
SPECGATE_SERVER=http://localhost:8080 bash app/cli/test/e2e/delivery-outcomes.sh
```

The handoff smoke uses a temporary `HOME`, logs in to a disposable workspace, installs
and checks IDE plugin assets, creates a quick work item, fetches its Context
Pack, runs gates, submits delivery evidence, checks the delivery verdict, and
archives the disposable work item during cleanup.

Set `SPECGATE_E2E_RUN_ID` to label a run; the scripts keep full labels on
workspace/test data and shorten the login username/email suffix to satisfy the
server validation contract.

The artifact-readiness smoke logs in with a disposable user/workspace, publishes
a role-tagged artifact package, verifies stored file content, runs artifact
readiness checks, and exercises IDE-agent gate task preview/dispatch.

The delivery-outcomes smoke temporarily points delivery review at a provider
without a configured key so the agents service takes the deterministic
coding-agent-claim fallback. It proves `needs_human_review` for partial
evidence, then proves a platform pass remains pending until explicit human
approval triggers auto-archive. It restores model and archive settings on exit.

For cloud-dependency e2e runs, layer `docker-compose.cloud.local.yml` after the
base and dev files. The override points Doc Registry at cloud Postgres, Redis,
and S3, and enables `KNOWLEDGE_DRIVER=pgvector` with the default 1024-dimension
Knowledge index so Governance Knowledge upload/search exercises the same
Postgres-backed vector path as a multi-machine deployment.

### Doc Registry

```bash
cd app/doc-registry
make test
make lint
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
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
uv run deptry src evals
```

Default tests use scripted models and do not require LLM keys. The governance
chat graph is a single governance-ops node with read-only tools. Model-backed
readiness, delivery review, and Full-mode quick-work acceptance-criteria
drafting run through explicit Python HTTP routes.

### UI

```bash
cd app/ui
npm run test -- --run
npm run lint
npm run build
npm run api:check
npm run deadcode
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

GitHub Actions workflows declare explicit `permissions`. Test-only workflows use
`contents: read`; release and Pages workflows request only the write scopes they
need for packages, releases, or Pages deployments.

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

### Knowledge Retrieval Gold Set

Run `cd app/doc-registry && go test ./internal/knowledge -run 'TestKnowledgeRetrievalGold' -count=1` after changing Knowledge chunking, ranking, filters, or citation contracts. The offline rows prove the ingest → scoped search → citation pipeline with a deterministic fake embedder; they do not measure semantic quality. For the bilingual (Vietnamese ↔ English) rows, run `KNOWLEDGE_LIVE_EVAL=1 go test ./internal/knowledge -run TestKnowledgeRetrievalGoldLiveRows -count=1` with a configured embedding model. Expand the gold set before tuning ranking weights, and never add a rerank stage without a before/after score from this set.

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
- [Governance reference](../using-specgate/reference/governance.md)
- [Operate SpecGate](../using-specgate/guides/operate-specgate.md)
- [Release guide](release.md)
