# Contributing to SpecGate

Thanks for your interest in SpecGate. This guide covers local setup, the per-module
checks, and how we expect changes to be shaped.

## Prerequisites

- Go 1.26+, Docker + Docker Compose, Node 26+
- `uv` (Python package manager) — `curl -LsSf https://astral.sh/uv/install.sh | sh`

## Local setup

```bash
make setup     # creates .env files, generates encryption key, starts all services
make seed      # optional: load demo governance data to explore the UI
```

`make setup` is idempotent. LLM provider keys are configured **in the app** after boot
(Settings → Model) — no API keys are needed to start.

## Branch and PR workflow

1. Fork the repo and create a branch from `main`: `git checkout -b feat/your-feature main`
2. One feature or fix per PR — keep the diff reviewable.
3. Fill in the PR template: what changed, which spec section it maps to, which checks you ran.
4. Open a PR against `main` (not `staging` or `cleanup`).

## Commit style

Use **conventional commits**: `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`, `perf:`.
Scope with the module name: `feat(doc-registry): add conflict detector`.

## Per-module checks

Run the relevant module's checks before opening a PR:

### `app/doc-registry` (Go)

```bash
cd app/doc-registry
make fmt       # gofmt -s -w
make lint      # go vet ./...
make test      # Go race suite; Postgres integration tests use testcontainers
```

### `app/agents` (Python, uv)

```bash
cd app/agents
uv run ruff check src tests    # lint
uv run ruff format src tests   # format
uv run pytest                  # unit + harness (excludes live smoke by default)
```

### `app/ui` (Vite + React)

```bash
cd app/ui
npm run lint            # ESLint
npm run build           # TypeScript check + production build
npm run test -- --run   # Vitest unit suite
```

## Code style

- **Go:** `gofmt -s` for formatting; `go vet` for analysis. No linter suppression without comment.
- **Python:** `ruff` for lint and format. Type hints required; no `Any` without justification.
- **TypeScript:** strict mode. Fix type errors — do not loosen `tsconfig`.

## Spec-driven workflow

This codebase is **spec-driven**: update the nearest relevant doc in the same change as
the code. See `AGENTS.md` under **Spec-driven changes** for the doc-layer guide.
A PR that changes behavior without updating the spec will be asked to add the
doc update before merge.

## Reporting bugs

Open a [GitHub issue](https://github.com/thanhtung2693/specgate/issues) using the **Bug report**
template. Include reproduction steps, the module(s) affected, and `docker compose logs`
output if relevant.

## Requesting features

Open a [GitHub issue](https://github.com/thanhtung2693/specgate/issues) using the **Feature
request** template. Describe the problem you are solving, not just the solution.

## DCO / CLA

No CLA is required. By submitting a PR you certify that the contribution is yours to
submit and that you agree to license it under the project's [Apache 2.0 license](LICENSE).
