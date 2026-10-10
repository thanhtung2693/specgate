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

Both Go modules select Go 1.27.1 through `toolchain` while retaining the Go
1.26.4 language/dependency floor. Use Staticcheck v0.8.1 with this toolchain;
v0.7.0 cannot read Go 1.27 export data. Compiler upgrades require both full Go
suites, Local data/legacy-writer dogfood, and rebuilt release image stages, not
only successful compilation.

For dependency audits, distinguish `go list -m -u all` from the modules reached
by `go list -deps -test ./...`. The module graph can include tools, alternate
platforms and dependency-owned tests not built by the affected module suite;
check their actual consumers before adding upgrade pins. Keep module checksum
verification and the affected full suite alongside any lockfile update.

Terminal-width dependency upgrades also need the CLI help/plain/JSON and
accessible prompt checks. Invisible format controls, combining characters and
grapheme clusters affect rendering even when the command contract is unchanged;
do not normalize or rewrite user input to compensate for display width.

All Dockerfiles pin the stable `docker/dockerfile:1.27.1` frontend, independent
of the Docker Engine and BuildKit versions. Upgrade the four syntax directives
together, run the release-readiness consistency check, and build each affected
image with the new frontend. Recheck isolated appliance startup after its full
build; syntax parsing alone does not establish runtime compatibility. Do not
switch to floating or labs tags to bypass a build failure.

Python appliance build-tool upgrades require rebuilding the `agents-build`
stage of `docker/Dockerfile.local` with its frozen production lock, then checking
the installed package and LangGraph HTTP app inside that Linux environment.
The build-only `uv` binary stays in `/usr/bin`; the final stage copies
`/usr/local` and the application environment, not the build tool. A stage build
does not establish full-appliance health or external-provider behavior.

The separable agents image also installs that frozen production lock. It mounts
the pinned `uv` binary only during build steps and puts `/deps/agents/.venv/bin`
on `PATH`; runtime does not need `uv`. Verify both its default `langgraph dev`
command and the supported `uvicorn` command override, installed dependency
versions against `app/agents/uv.lock`, and absence of populated `.env` files or
the build tool. Use isolated container state and disable tracing and provider
inference for these packaging checks.

The appliance pins s6-overlay 3.2.3.2. Its noarch and architecture-specific
archives are verified against the upstream SHA-256 files during the image
build. Supervisor upgrades require a full appliance build and isolated startup
check with fresh data; a successful stage build alone does not verify service
ordering, health, or shutdown. Never reuse a user's appliance volume for this
check.

Supervisor ownership or lifecycle changes also require the disposable-image
regression: `SPECGATE_TEST_APPLIANCE_IMAGE=<built-image> node --test docker/local/supervisor.test.mjs`.
It exercises the current startup/finish scripts inside the selected image,
checks component write denial, malicious counters and symlinks, healthy resets,
and planned shutdown. The test uses no host data volume and removes its container.
Without an explicit image the test is skipped; that is not release evidence.

The appliance builds gosu 1.19 with Go 1.27.1, `golang.org/x/sys` v0.48.0
and `github.com/moby/sys/user` v0.4.1
in an isolated build module rather than shipping upstream's older compiled
binary. Verify its installed build metadata and non-root execution, then run
the fresh appliance startup check; PostgreSQL uses gosu during initialization.

Appliance and separable contributor images use pgvector 0.8.7 on PostgreSQL 18
Trixie. Verify the installed extension in a fresh disposable database and run
the vector-store tests. An image update does not itself run `ALTER EXTENSION`
against existing databases; do not mutate a user's extension state as part of
verification.

For pgvector image upgrades, retain a disposable old-image database across a
clean shutdown and new-image start. Check existing vector rows, index-backed
queries and blob bytes before and after an explicit fixture-only extension
upgrade. Fresh-database startup alone does not establish upgrade compatibility.

## Module commands

### CLI

```bash
cd app/cli
make test
make lint
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
```

The CLI suite covers command behavior, JSON envelopes, exit codes, local
configuration, user/workspace selection, plugin installation, and uninstall
cleanup.

CLI command fixtures isolate streams, config, working directory, and home.
Select test HTTP servers explicitly with `--server` or saved configuration;
the shared `newTestDeps` helper does not select a server or inject a client.
Direct printer fixtures use `NewWithColor` with an explicit ANSI capability;
root-command tests still exercise terminal/environment capability detection.
Local delivery-decision fixtures call `DecideDeliveryWithBasis`, the production
entry point; an empty digest exercises the supported unenhanced review-ID path.

Enhanced Local-store changes also require a real pre-enhancement CLI, not only
raw SQLite guard tests. CLI CI and release verification run the shared gate:

```bash
bash .github/scripts/check-legacy-cli.sh
```

It builds the immutable pre-enhancement v0.1.4 revision in a temporary directory
and runs both compatibility tests with that executable. A shallow checkout
fetches only the required commit; it does not switch or reset the current
checkout. Temporary baseline files are removed on exit. Updating this baseline
requires reviewing that it still lacks enhanced-store support, not choosing the
newest version tag. The normal module suite without the gate may still skip
these opt-in tests.

For a separately built old executable, run:

```bash
cd app/cli
SPECGATE_LEGACY_CLI=/absolute/path/to/old/specgate go test ./internal/local -run 'Test(LegacyBinaryCannotMutateEnhancedStore|PreEnhancedBackupRestoresLegacyWritableSnapshot)$' -v -count=1
```

The test isolates home, config and database, checks that the binary starts,
then attempts old startup/read, workspace/work creation, verification, review
and acceptance against an enhanced store. Each must encounter the capability
guard without changing any stored rows. Without the environment variable these
tests are skipped; the normal suite alone is not evidence of downgrade safety.

The recovery test copies the pre-enhanced backup into a separate temporary
directory, verifies that the old CLI can write there, and compares the original
work and Context Pack with the restored data. The first checkpoint and later
work are absent from that snapshot; the live upgraded database and original
backup remain untouched. This verifies snapshot recovery, not a lossless
downgrade or a procedure for overwriting a user's running database.

The barrier-only `EnableEnhancedStore` fixture exists only in test builds.
Production enhanced writes use `WithEnhancedWrite` so the compatibility marker,
mutation guards, and first enhanced record commit together.

When upgrading `modernc.org/sqlite`, keep `modernc.org/libc` at the exact version
required by that driver's `go.mod`. Rerun the full CLI suite, the released-binary
compatibility check, and Local read/write dogfood; a successful build alone does
not verify stored-data compatibility.

JUnit parser fixtures open temporary directory handles and exercise
`ObserveJUnitReportFromRoot`, the same reader used by delivery checks. Production
opens that handle before executing the runner; parser-only fixtures do not
prove the runner's path-replacement boundary. Keep command-level boundary tests
when changing report collection.

Native Windows updater or browser-opening changes also require the
Windows-target build check:

```bash
cd app/cli
GOOS=windows GOARCH=amd64 go build -o /tmp/specgate.exe ./cmd/specgate
GOOS=windows GOARCH=amd64 go test -c ./internal/command -o /tmp/specgate-command.test.exe
```

The CLI and release workflows run updater, browser-boundary and open-command regression
tests on `windows-latest`; the release job cannot build assets until that native
check passes. Cross-compilation does not run Windows tests or verify the native
browser integration. The browser tests inject ShellExecute to verify the fixed
verb, exact URL document argument, absent parameters and native-error handling
without opening a real browser.

With a local stack running, the CLI also has opt-in new-user e2e smokes:

```bash
SPECGATE_SERVER=http://localhost:3000/api/doc-registry bash app/cli/test/e2e/handoff.sh
SPECGATE_SERVER=http://localhost:3000/api/doc-registry bash app/cli/test/e2e/artifact-readiness.sh
SPECGATE_SERVER=http://localhost:3000/api/doc-registry bash app/cli/test/e2e/delivery-outcomes.sh
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
readiness checks, and exercises IDE-agent gate task dispatch.

The delivery-outcomes smoke temporarily points delivery review at a provider
without a configured key so the agents service takes the deterministic
coding-agent-claim fallback. It proves `needs_human_review` for partial
evidence, then proves a platform pass remains pending until explicit human
approval triggers auto-archive. It restores model and archive settings on exit.

### Doc Registry

```bash
cd app/doc-registry
make test
make lint
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
```

The Doc Registry suite covers API handlers, database behavior, artifact state,
events, evidence, settings, and contract fixtures. Tests use testcontainers for
Postgres where needed.

After upgrading the Postgres driver or testcontainers dependencies, confirm
that the Postgres subtests actually run: they can skip when Docker is
unavailable. Run an affected database test with `-v -count=1` and inspect its
`postgres` subtest result; a green package result with skipped database tests
is not evidence of migration or persistence compatibility. These tests create
disposable containers and databases, not databases in the contributor stack.

For AWS SDK upgrades, `TestClientRoundTripThroughConfiguredEndpoint` exercises
signed put/get/delete requests through a loopback HTTP endpoint with fake
credentials and isolated shared-config paths. It verifies path-style routing,
object bytes and content type, not real S3/MinIO compatibility or response
checksum authentication. Queue handler fixtures do not connect to Redis;
Redis-client upgrades also need a disposable live queue check before claiming
runtime compatibility. Never use contributor buckets or queues for these checks.

Run the opt-in real Redis roundtrip after a Redis-client upgrade:

```bash
cd app/doc-registry
go test -tags integration -race -count=1 ./internal/knowledgequeue -run '^TestRedisQueueRoundTrip$' -v
```

This starts a fresh Redis 8 testcontainer with tmpfs data, sends both Knowledge
and webhook tasks through the production enqueuers and handlers, and checks
unchanged workspace/payload plus successful queue acknowledgements. It fails
rather than skips if Docker is unavailable. It does not invoke embeddings or
providers, and does not establish crash recovery, retries or throughput.

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

The gate checks v0.1 positioning, installer paths, Compose defaults, image
runtime settings, Node workflow versions, static landing metadata, terminology,
and contract references.

GitHub Actions workflows declare explicit `permissions`. Test-only workflows use
`contents: read`; release and Pages workflows request only the write scopes they
need for packages, releases, or Pages deployments.

## What to verify by change type

| Change | Minimum useful proof |
|---|---|
| CLI command behavior | Targeted CLI tests, then `make test` in `app/cli`; add the Windows-target build for platform-specific updater changes |
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
curl -s http://localhost:3000/api/agents/threads/<thread-id>/state
curl -s http://localhost:3000/api/doc-registry/api/v1/status
curl -s http://localhost:3000/api/doc-registry/api/v1/work-items
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
