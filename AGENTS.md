# SpecGate Contributor Rules

These are repository rules for people and coding agents who modify, test,
review, or release SpecGate. They are not instructions installed for SpecGate
product users. Product installation and day-to-day workflows belong in
[`docs/using-specgate/`](docs/using-specgate/README.md).

## Rule precedence

1. Read this file before modifying the repository.
2. Read the nearest nested `AGENTS.md` for every module you touch. Nested files
   extend this file and may add stricter module rules.
3. `CLAUDE.md` is only a Claude Code entry point to these contributor rules; it
   must not duplicate or replace them.

Product plugin rules under `plugins/` and their generated copies describe how
installed IDE agents use SpecGate. They are product assets, not contributor
rules for this repository.

## Repository map

| Path | Stack | Responsibility | Contributor docs |
| --- | --- | --- | --- |
| `app/doc-registry/` | Go 1.26+ | REST API, governance state, Postgres, and object storage | `app/doc-registry/AGENTS.md`, `app/doc-registry/docs/` |
| `app/agents/` | Python 3.12+ / LangGraph | Governance chat and model-backed governance operations | `app/agents/AGENTS.md`, `app/agents/docs/` |
| `app/ui/` | Vite / React / TypeScript | Web application | `app/ui/AGENTS.md`, `app/ui/docs/` |
| `app/cli/` | Go 1.26+ / Cobra | Human- and IDE-agent-facing CLI, Local mode, and installers | `app/cli/AGENTS.md`, `docs/using-specgate/reference/cli.md` |
| `app/landing/` | Static HTML/CSS/JS | Public landing page | `app/landing/README.md` |
| `plugins/` | Markdown, scripts, manifests | Canonical IDE plugin assets | `plugins/README.md` |
| `deploy/`, `docker/` | Compose / Docker | Released and contributor deployment packaging | `deploy/README.md`, `docs/contributing/setup.md` |

Contributor architecture, contracts, setup, testing, and release guidance live
in [`docs/contributing/`](docs/contributing/README.md). Product documentation
lives in `docs/using-specgate/`.

## Before editing

- Read `docs/contributing/README.md` and `docs/contributing/setup.md` on the
  first contribution.
- Read the owning module's README, specification, and nested `AGENTS.md`.
- State material assumptions and clarify ambiguity that would change the
  product contract or data-safety outcome.
- Prefer the smallest change that satisfies the request. Do not add speculative
  abstractions, refactor unrelated code, or silently clean up pre-existing
  dead code.
- Prefer current stable dependency and tool versions when adding or upgrading
  them, unless a documented compatibility constraint requires a pin.
- Preserve unrelated work in a dirty worktree.

## Spec-driven changes

Code and its owning documentation change together. Do not postpone docs to a
later pass or refuse to implement because a contract entry is missing; update
the correct layer in the same change.

| Change | Owning documentation |
| --- | --- |
| Product intent, goals, or non-goals | Owning module PRD |
| API, state, event, policy, retention, or behavior contract | Owning module `docs/spec.md` |
| Cross-module vocabulary or behavior | `docs/contributing/contracts.md` |
| Architecture decision or trust boundary | `docs/contributing/architecture.md` or an ADR |
| Contributor setup, testing, or release process | `docs/contributing/` |
| Product installation or user workflow | `docs/using-specgate/` |
| Environment variable | Owning `.env.example`, config tests, and setup/reference docs |

Use repository-relative paths in documentation. Reference a spec section in
code comments only when it materially helps a future contributor.

### Documentation placement

`docs/contributing/` contains durable guidance, not per-change artifacts. Keep
implementation plans, handoff notes, review scratch, completion receipts, and
other transient agent output in the conversation or under the gitignored
`.specgate/` work area. Do not use `docs/superpowers/` or another framework's
default planning path unless the user explicitly requests a durable,
repository-local document.

Specifications authored with OpenSpec, Spec Kit, Superpowers, or another
framework remain in the author's selected location. SpecGate may register them;
it does not relocate them into contributor documentation.

## Implementation and tests

- Use test-driven development for behavior changes: reproduce the failure or
  add the contract test, implement the smallest fix, then verify it passes.
- Follow existing test patterns and isolate environment/filesystem state with
  tools such as `t.Setenv`, `t.TempDir`, and temporary homes.
- Run the narrowest meaningful check while iterating. Run the full affected
  module suite when changing shared state, routing, persistence, build
  configuration, or a cross-module contract.
- Visual changes require a rendered browser check at desktop and one narrow
  viewport in addition to targeted automated tests.
- If verification cannot run, report exactly what was skipped and why.

The canonical command matrix and escalation guidance are in
[`docs/contributing/testing.md`](docs/contributing/testing.md). Typical module
checks are:

| Module | Commands |
| --- | --- |
| CLI | `cd app/cli && make test && make lint` |
| Doc Registry | `cd app/doc-registry && make test && make lint` |
| Governance operations | `cd app/agents && uv run pytest -q && uv run ruff check src tests` |
| UI | `cd app/ui && npm run test -- --run && npm run lint && npm run build` |
| Landing page | `node --test app/landing/landing.test.mjs && node -c app/landing/script.js` |
| Release-facing or cross-module change | `node --test docs/release-readiness.test.mjs` |

## Cross-module and generated assets

- Communicate across modules through documented REST/event contracts. Do not
  import another module's internals or couple directly to its database/object
  store.
- Update `docs/contributing/contracts.md` when shared statuses, event shapes,
  endpoints, or names change.
- Canonical plugin assets live under `plugins/`. After changing them, run
  `make sync-plugins` and `make check-plugins`, then include synchronized
  generated copies in the same change.
- Do not hand-edit generated plugin copies under CLI or Doc Registry packages.

## Safety and data protection

- Never commit secrets, tokens, private keys, credentials, or populated `.env`
  files. Inspect staged changes before committing.
- Never log complete credentials, JWTs, signed URLs, or secret-bearing request
  bodies.
- Never fabricate repository paths, API references, commands, or verification
  results.
- Treat user specifications, `.specgate/` state, local databases, object
  storage, Docker volumes, IDE configuration, and home-directory plugin files
  as user data. Cleanup and uninstall behavior must be explicitly scoped,
  previewable where practical, and covered by tests proving unrelated files
  survive.
- Never drop databases, purge buckets or volumes, remove user files, rewrite IDE
  configuration, or perform equivalent destructive operations without explicit
  confirmation and a verified target.
- Never run `git reset --hard`, force-push a shared branch, bypass hooks, or
  discard unreviewed work.
- Confirm before pushes, merges, PR creation, external comments, releases, or
  any action with blast radius beyond the local checkout.
- Diagnose the root cause of failing checks; do not bypass them.

## Git and review hygiene

- Create commits only when requested.
- Use conventional commits with a useful module scope, for example
  `fix(cli): preserve user specs during uninstall`.
- Keep diffs surgical. Remove only imports, variables, tests, or docs made
  obsolete by the current change; report unrelated dead code separately.
- Never amend a published commit. Never skip hooks.
- Before completion, review the diff, run the appropriate checks, and report
  evidence rather than assuming success.

## Governance-chat regression discipline

For real-backend governance-chat streaming failures, diagnose the live path
before editing UI code. Compare browser assistant state with the matching
LangSmith trace for the same `thread_id`, probe `/runs/stream`, and follow the
container reload procedure in
[`docs/contributing/testing.md`](docs/contributing/testing.md#langgraph-streaming-probe).
Do not hide real streaming failures behind scenario-specific UI mocks.
