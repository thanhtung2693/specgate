# SpecGate — Agent Rules

Canonical rules for AI coding agents (Claude Code / Cursor / Codex CLI / others) working in this monorepo. Tool-specific entry points (`CLAUDE.md`, `.cursor/rules/main.mdc`) reference this file; nested `AGENTS.md` in each module override / extend these rules for the module.

## 1. Project overview

**specgate** is a monorepo for an AI-assisted SDLC system. Three top-level modules:

| Module                    | Language             | Entry point                              | Docs                                        |
| ------------------------- | -------------------- | ---------------------------------------- | ------------------------------------------- |
| `app/doc-registry/`       | Go 1.26+             | `cmd/doc-registry/main.go`               | `app/doc-registry/docs/`                    |
| `app/agents/`             | Python / LangGraph   | `langgraph dev` / `src/specgate_agents/` | `app/agents/docs/`, [app/agents/README.md](app/agents/README.md) |
| `app/ui/`                 | Vite + React + TS    | `ui/src/main.tsx`                        | `app/ui/docs/`, `app/ui/README.md`          |

Monorepo-level docs live in `docs/` (quickstart, features, concepts/, guides/, reference/, contracts, data model, testing, internals/).

## 2. Workflow: spec-driven (strict)

The project follows a **document layering** model — PRD (intent), Spec (contract), README (dev flow) — and it is treated as **spec-driven**. Agents must keep the relevant docs updated in the same change as code.

- Implement according to `spec.md` for the module.
- **Update the relevant docs as part of every code change**. Treat this as required, not optional.
- Use the narrowest doc layer that matches the change: PRD for intent, Spec for contract/behavior, README for dev flow, ADR/architecture for system decisions.
- **Reference** spec sections in code comments when it aids the reader, e.g. `// per spec §14`.
- Do not refuse to write code because a spec entry is missing — add the missing doc update in the same task, flag the gap to the user, and proceed.

## 3. TDD default

- Write a failing test before implementing (or update tests in the same change as the code).
- Verify tests fail before the fix, pass after.
- Respect existing test patterns in the module (e.g. `t.Parallel()`, `t.Setenv(...)` in Go).
- Match verification cost to change size. Evidence before assertion still applies, but do not default to the most expensive command for a small, local change.
- For small copy, styling, spacing, or isolated component tweaks, prefer the narrowest proof that can actually fail for the touched code: targeted test file(s) plus a browser/manual check when visual behavior matters.
- Run the full module test suite before claiming a task complete when the change affects shared state, data flow, routing, build configuration, cross-module contracts, or other areas with meaningful regression surface.
- If the user asks for a quick UI pass or a narrow fix, avoid broad lint / build / full-suite runs unless they are necessary for the touched surface or the user explicitly wants the heavier verification.
- If a module has no test runner yet (see module AGENTS.md), write tests using whatever the module's AGENTS.md recommends; do not invent one silently.
- Treat doc updates as part of the definition of done for code changes, alongside tests.
- **Real-backend governance-chat bugs: diagnose before fixing** — see
  [app/ui/AGENTS.md](app/ui/AGENTS.md) §"Governance-chat regression discipline".
  Don't add scenario-specific synthetic mocks for failures that depend on real
  streaming behavior. Before choosing UI vs backend, inspect both the browser
  assistant-ui state and the matching LangSmith trace (`message_log`,
  `final_response`, companion events, canvas payload) for the same `thread_id`.

## 4. Working style

These behavioral guidelines reduce common LLM coding mistakes. **Tradeoff:** they bias toward caution over speed. For trivial tasks, use judgment.

### Think before coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them — don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### Simplicity first

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### Surgical changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it — don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### Goal-driven execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan with explicit verification for each step. Before running expensive verification (full test suite, build, broad browser sweep) or broad adjacent work beyond the request, pause and say what you plan to run and why. Before declaring success, run the relevant verification and confirm the result; if it cannot be run, say exactly what was not verified and why.

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## 5. Doc update checklist

Layered — update the smallest relevant doc, but always update docs when code changes:

| When                                              | Update                    |
| ------------------------------------------------- | ------------------------- |
| Product intent / goals / non-goals change (rare)  | `docs/prd.md` (module)    |
| API shape / status lifecycle / event schema / retention policy / contract change | `docs/spec.md` (module)   |
| Local dev flow — commands, URLs, setup            | `README.md` (module)      |
| Env var added / removed / renamed                 | `.env.example` + README   |
| Architectural / cross-module decision             | `docs/adr/`                           |

**Not required** for: choosing the wrong doc layer. Always update the right doc for the change.

## 6. Safety guardrails

- **Never** commit secrets, API keys, JWTs, or credentials. Scrub `.env` before staging.
- **Never** run `git push --force` to shared branches, `git reset --hard` on unreviewed work, `rm -rf data/` or equivalent destructive ops without explicit user confirmation.
- **Never** skip git hooks (`--no-verify`) or bypass signing unless explicitly asked.
- **Never** fabricate URLs, file paths, or API references.
- **Always** confirm before actions with blast radius beyond local (push, PR create, merge, comment on external systems).
- **Prefer latest stable** versions for dependencies, Docker base images, and CLI tooling when adding or upgrading.
- When stuck, investigate root cause instead of bypassing — e.g. fix failing hook rather than `--no-verify`.

## 7. Git / commit protocol

- Create commits only when the user asks. Do not batch unrequested "cleanup" commits.
- Use conventional commits: `feat: / fix: / chore: / docs: / refactor: / test: / perf:`.
- Scope = module name: `feat(doc-registry): add conflict detector`, `docs(ui): clarify shadcn workflow`.
- Reference spec section if the change maps to one: `fix(doc-registry): retain needs_changes in retention (per spec §9)`.
- Never amend a published commit; always create a new one.
- Never skip hooks. If a hook fails, fix the underlying issue.

## 8. How to work in this repo

- Read the module's `AGENTS.md` in addition to this file when touching that module.
- Read `docs/README.md` and `docs/quickstart.md` first time in a new session.
- For cross-module changes, update `docs/contracts.md` if shared statuses / events / naming change.
- If code changes and you cannot identify a doc to update, pause and ask rather than silently skipping docs.
- When in doubt about scope or approach: ask the user, do not guess at intent.

## 9. Streaming / SSE debugging

For any governance-chat bug that looks like "transcript blank for N seconds, then
the response pops in at once", **probe `/runs/stream` on the live LangGraph
API before editing UI code**. See [docs/testing.md §LangGraph Streaming SSE
Probe](docs/testing.md#langgraph-streaming-sse-probe-runsstream) for the
scalar-per-mode `curl` matrix, the multi-mode array check, and the
"normalizer fold" vitest pattern that proves end-to-end.

Container code refresh rule: the self-hosted `langgraph` server in
`specgate-agents-1` imports the graph **once at startup and does not hot-reload
Python**. `docker compose watch agents` only **syncs the file** into
`/deps/agents/src` (the `develop.watch` block uses `action: sync`, not
`sync+restart`) — the running server keeps the old module in memory, so a synced
`.py` edit is NOT live. To load Python changes you must
`docker restart specgate-agents-1` (wait for `health=healthy`, ~10s). Restart
re-imports the watch-synced `/deps/agents/src` editable install, so it DOES pick
up your changes — it does not reset to the baked image. (`langgraph build`
without a sync would leave the baked old source; that is the separate case where
a bare restart ignores changes.) Use `langgraph up --image ... --wait`,
`langgraph up --watch`, or native `langgraph dev` when you want auto-reload or a
prod-shaped image. See `docs/testing.md` for all four commands.

**Do not trust `inspect.getsource` via `docker exec ... python -c` as proof the
server reloaded** — that spawns a fresh Python process that re-reads the synced
file, so it shows your edits even while the long-running server still runs the
old code. Verify against the running server instead: exercise the code path and
check behavior / `/threads/{id}/state`, or restart first, then test.

## 10. This repo dogfoods SpecGate

Changes here flow through SpecGate as governed work items. When you pick one up, follow the installed **`using-specgate`** skill — it carries the readiness checks and the pickup → implement → delivery-review loop. That product-usage detail lives in the skills and [docs/guides/coding-agent-workflow.md](docs/guides/coding-agent-workflow.md), not in these coding rules.
