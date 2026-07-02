# OSS release readiness checklist

Go/no-go checklist for open-source releases, across **documents, code,
security, and CI**. Complements the executable gate in
[`../release-readiness.test.mjs`](../release-readiness.test.mjs).

Status legend: ✅ done · ⚠️ recommended before release · ⛔ blocker.

Last updated: 2026-07-02 (simplification release).

## This release

- ✅ Speculative "ambition layer" deleted across all modules (~16k LOC):
  reconciliation drafting, lifecycle suggestions, extractions, the standalone
  evidence-manifest protocol, corpus benchmarking, outcome/policy-health
  calibration, admin api-keys, policy-v1 registry/resolver, 30 registry
  endpoints, 11 agents routes, 6 dead tables, 3 CLI command families. The live
  delivery-evidence path (completion report → feedback events → delivery
  review) is untouched and now the only evidence door.
- ✅ Artifact lifecycle collapsed 9 → 4 statuses (draft, needs_changes,
  approved, superseded) with migration-asserted zero legacy rows; event enum
  trimmed to live writers; contracts.md aligned.
- ✅ CLI delivery tail: `delivery submit` (report → gates → review → verdict in
  one command), `delivery report --init` scaffolds completion.json with real
  criterion ids, confirms are TTY-only, `work create-quick "Title" --ac` flags.
- ✅ UI gains its human verbs: approve/reject artifact proposals in Reviews
  (save/discard), Approve / Request changes on draft artifacts (status PATCH,
  actor human); review rows deep-link to the Verification tab.
- ✅ UI right-sized: identity auto-bootstraps (no onboarding gate, no logout),
  Settings 7 → 5 sections (Workspace/Knowledge panels removed, Plugins is a
  help card, Skills CRUD primary in Governance, Integrations details lazy),
  dead-weight sweep (~500 lines) + tone-mapper consolidation.
- ✅ Chat kept but stripped to 4 artifact/readiness tools; unconfigured chat
  model shows a capability placeholder with add-key instructions (new
  `/governance/chat/health` probe).
- ✅ Skills consolidated 7 → 4 (`using-specgate`, `setting-up-specgate-project`,
  `preparing-work`, `delivering-work`) adopting the new delivery flow.
- ✅ Post-review simplification: gate vocabulary unified into one `gates` family;
  the gate/readiness stores unified into one `gate_runs` table (byte-identical
  contracts, live-DB fold); `specgate stats` governance-value readout with a
  Work-board stats card; five never-queried tables dropped from the schema.
- ✅ API "dialect merge" resolved by convention, not code: investigation showed
  the versioned `/api/v1` CLI facade and the unversioned internal API are an
  intentional audience split (only ~3 nouns overlap, and those serve different
  clients), so a wholesale merge would remove the CLI's version contract for no
  gain. The split is now documented in `docs/contracts.md`.
- The governance chat panel is a kept product surface (stripped to
  artifact/readiness tools, with the add-key placeholder when unconfigured).
- Small follow-ups (optional, low value): dedupe the `skills` noun to one
  dialect (the UI reads `/api/v1/skills` but writes `/skills`; flipping it
  cleanly touches the UI, both agents callers, and the registry routes in
  lockstep); and let `specgate stats` pre-build catches count artifact
  readiness runs now that the stores are unified.

## Documents

- ✅ README positions the release as alpha, CLI-first, UI available
  (enforced by `release-readiness.test.mjs`).
- ✅ Quickstart matches the real install path (`install-cli.sh`, `specgate init`,
  `plugins/install.sh`).
- ✅ Concept, guide, and reference docs present and cross-linked from
  `docs/README.md`.
- ✅ Retired terminology absent from tracked sources (enforced by the gate).
- ✅ Model settings documented as the **server-side** model (gates, route
  classification, summaries, delivery review); the governance chat agent's model
  is env-configured. Renamed from the ambiguous "governance model" in UI + docs.
- ✅ `LICENSE` (Apache-2.0), `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  `SECURITY.md`, `LICENSING.md` present.

## Code

- ✅ All four modules build and pass their suites: `app/cli` (go test),
  `app/doc-registry` (go test -race), `app/ui` (build + lint + vitest),
  `app/agents` (ruff + pytest).
- ✅ Dead-code pass (reference-traced, boundary-vetted):
  - CLI: removed unused `ports.go` port-conflict cluster, `Printer.Fprintf`,
    `Service.Dir()`.
  - doc-registry: removed unused `writeIntegrationHTTPError` and the unregistered
    `CompleteIntegrationOAuth` huma handler + its schema types (live OAuth callback
    is the inline chi route, which keeps its end-to-end test).
  - UI: removed 3 unused shadcn files (`command`, `input-group`, `sonner`), 4
    unused deps (`cmdk`, `sonner`, `motion`, `@assistant-ui/react-markdown`),
    genuinely-dead sample-data exports, stale sample source tags, static
    integration/mention fallback catalogs, and unused public SVG assets;
    declared the previously-undeclared `assistant-stream` dependency.
- ✅ Bug fix: `gates run` / `delivery review` with `--json` (implies
  `--no-input`) but no `--yes` now emit a clear error envelope instead of failing
  silently (empty output, exit 2). Covered by tests.
- ✅ Test-isolation fix: CLI command tests no longer write to the developer's
  real `~/.config/specgate/config.json`.
- ✅ Governance chat surface is thin (single graph, governance-ops tools; no
  legacy planner/drafting bulk).
- ✅ Full governed delivery loop validated end-to-end (2026-07-02 dogfood,
  CLI + UI): `work create-quick "Title" --ac` → Context Pack → implement →
  `delivery report --init` → `delivery submit` → verdict `pass` (2/2 criteria
  met), including the rework arc (needs_human_review → fix → pass), artifact
  Approve in the UI, and the chat add-key placeholder live. Seven seam bugs
  found and fixed during the pass (board 500 on feature-less quick items,
  scaffold envelope fields, criterion-id correlation, human-mode JSON errors,
  pack instruction drift).
- ✅ Landing page frames SpecGate as alpha, CLI-first, trusted-network
  software; verification language is evidence-based rather than overclaiming
  automatic proof.
- ✅ Post-squash release scan: repeated the stale-term, placeholder, sample-data,
  onboarding, README, guidance-doc, and landing metadata sweep after rewriting
  `main` to one release commit. Fixed the landing social metadata to point at
  the shipped `logo.svg` asset and added coverage in both the landing and
  release-readiness gates.
- ✅ This release resolved the prior dormant-subsystem flags: the agents `tools/mcp.py`
  loader and the doc-registry policy-v1 resolver were removed (the registry's
  MCP layer that the governance chat actually uses is untouched).
- ⚠️ `App.test.tsx` (~4.7k lines) remains a monolith; candidate for a split in
  a future pass. `app-shell.tsx` stays split into domain modules (~2.4k lines).

## Security

- ✅ `.env` gitignored and untracked; no secrets in git history.
- ✅ `.env.example` stays lean/settings-first (enforced by the gate).
- ✅ Machine-local artifacts (`.mcp.json`, `CLAUDE.specgate.md`, deployment env)
  gitignored (enforced by the gate).
- ✅ Doc Registry has no HTTP auth by design (trusted-network); READMEs warn
  against exposing it publicly.
- ✅ Release images use production-safe runtime defaults (enforced by the gate).

## CI

- ✅ Per-module workflows with path filters: `cli.yml` (make test),
  `doc-registry.yml` (go test -race), `agents.yml` (ruff + pytest),
  `plugins.yml` (make check-plugins), `ui.yml` (npm ci -> lint + build + test on
  Node 26).
- ✅ `release.yml` builds CLI via GoReleaser, uploads the compose bundle, and
  publishes the release container images.
- ✅ `release-readiness.yml` runs the encoded release gate
  (`node --test docs/release-readiness.test.mjs`) on every push/PR, so
  terminology and packaging regressions fail the build.

## Blockers

None currently identified. The ⚠️ items are recommended hardening, not blockers,
for an alpha release.
