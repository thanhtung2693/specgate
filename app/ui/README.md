# SpecGate UI

Vite + React + TypeScript app shell for the SpecGate (Experimental) governed delivery UI.

This folder contains the responsive app shell, vertical sidebar, light/dark theme tokens, route surfaces, and an assistant-ui governance agent surface. The UI is available for human review, artifact inspection, governance chat, settings, and operational scanning; authoring and implementation still start from the IDE + CLI workflow.

## Setup

```bash
cd app/ui
npm install
npm run dev
```

Copy `.env.example` to `.env.local` when running against a local Doc Registry:

```bash
VITE_DOC_REGISTRY_URL=/api/doc-registry
VITE_LANGGRAPH_API_URL=/api/agents
SPECGATE_PROXY_TARGET=http://127.0.0.1:3000
```

Set `SPECGATE_PROXY_TARGET` for gateway mode when the SpecGate appliance is
running. Both API prefixes are forwarded intact to the gateway, which routes
them to the appropriate service.

For raw-service mode, leave `SPECGATE_PROXY_TARGET` unset and configure the two
service targets separately:

```bash
VITE_DOC_REGISTRY_URL=/api/doc-registry
VITE_LANGGRAPH_API_URL=/api/agents
DOC_REGISTRY_PROXY_TARGET=http://127.0.0.1:8080
LANGGRAPH_PROXY_TARGET=http://127.0.0.1:2024
```

The browser-facing `VITE_*` URLs use Vite same-origin proxy paths in development.
In raw-service mode, Vite strips `/api/doc-registry` and `/api/agents` before
forwarding requests. Override the raw-service targets when your local compose
ports differ, for example when another stack already owns `8080` or `2024`.

When `VITE_DOC_REGISTRY_URL` is unset, the shell explains that the browser UI
requires Full mode instead of fabricating a browser-local workspace. Configure
the registry URL, then create or seed work through the CLI to populate the
surfaces. When the URL is configured but the registry is unreachable or empty,
live-mode surfaces show explicit loading, empty, or error placeholders instead
of bundled sample rows, diffs, document bodies, or acceptance criteria.

Governance chat is optional. The launcher is hidden unless
`VITE_LANGGRAPH_API_URL` is configured and its health endpoint confirms chat is
available. A missing or unreachable chat service does not block the Full-mode
work, review, artifact, Knowledge, or settings surfaces. The same URL is used
for governance custom routes; Context Pack readback stays on the Doc Registry
URL.

## Scripts

| Command | Purpose |
| --- | --- |
| `npm run dev` | Start the Vite dev server |
| `npm run lint` | Run oxlint |
| `npm run test` | Run Vitest component tests |
| `npm run build` | Type-check and build production assets |
| `npm run api:generate` | Regenerate the committed OpenAPI document and TypeScript contract |
| `npm run api:check` | Fail when the committed API contract differs from Doc Registry |
| `npm run deadcode` | Find unused UI files, exports, and dependencies with Knip |
| `npm run preview` | Preview a production build |
| `npm run docker:build` | Build the production nginx image as `specgate-ui:latest` |
| `npm run docker:run` | Run the image on [http://localhost:3000](http://localhost:3000) |

The production build keeps Mermaid in a separate lazy-loaded vendor chunk for document previews. The Vite chunk-size warning limit is set above that expected Mermaid payload so new warnings point to unexpected app-bundle growth.

CI runs these in [`.github/workflows/ui.yml`](../../.github/workflows/ui.yml):
`npm ci`, API contract drift, dead-code analysis, lint, build, and tests on Node
26. Changes to the Doc Registry API surface also trigger this workflow.

## Docker

The production image is built from the monorepo root with [`../../docker/Dockerfile.ui`](../../docker/Dockerfile.ui). It compiles the Vite bundle and serves it through nginx with SPA route fallback, so deep links like `/work/SG-155` load correctly. In release Compose, nginx also proxies `/api/doc-registry` to the Doc Registry service and `/api/agents` to the agents service.

From this folder:

```bash
npm run docker:build
npm run docker:run
```

The source-development Compose stack already includes the UI service:

```bash
docker compose up --build ui
```

In the dev Compose override, `node_modules` lives in a named Docker volume so
the source bind mount does not hide container-installed packages. The dev UI
container refreshes that volume on startup when `package.json` or
`package-lock.json` changes.

Vite `VITE_*` values are baked into the production image at build time. The release image uses `app/ui/.env.production`, which points the browser at the same-origin nginx proxy paths.

## Routes

First launch asks for attribution (display name, username, optional email, and
workspace). This is audit identity, not authentication or access control. Setup
must bootstrap the selection through Doc Registry; a failure keeps setup open
and points the user to `specgate doctor`. Returning sessions reopen the last
workspace. If that workspace no longer exists on the connected appliance, the
shell clears the stale browser selection and requires explicit setup instead of
silently creating registry records. A failed workspace listing leaves the saved
shell visible with unavailable live data.

`/` redirects to `/work`.

Shell routes:

- `/work`
- `/work/:itemKey`
- `/reviews`
- `/artifacts`
- `/knowledge`

Work is the first shell destination because implementation and handoff start in
the IDE and CLI. Reviews, Artifacts, and Knowledge support the human inspection
and governance steps around that flow. Data tables keep accessible captions and
switch to readable stacked rows on narrow screens (or a scrollable layout when
columns genuinely need more room).

A current platform review can be accepted or sent back with **Request changes**
from Work detail regardless of its advisory evidence verdict. The confirmation
names the exact work item and reviewed completion, attributes the decision to
the selected username, and requires useful feedback when changes are requested.
Doc Registry remains authoritative for stale, duplicate, self-review,
missing-review, and workspace errors.

The browser title and sidebar product title are `SpecGate (Experimental)`, and the favicon uses the shared SpecGate logo asset from `public/logo.svg`.

Settings is an honest status/configuration modal opened from the sidebar footer, not a standalone page. IDE plugin setup stays in the CLI and IDE; server-side governance plus embedding model settings save through Doc Registry `/settings`. Workspace/user identity actions live in the sidebar account menu, and the browser does not expose Knowledge or Plugins settings while those remain backend and IDE concerns. Integrations can add GitHub, GitLab, or Linear through Doc Registry only for the selected workspace and store a write-only provider API token; switching workspace clears integration details and pending dialogs. Settings groups GitHub and GitLab as Repositories, and Linear as Work tracking. Hosted OAuth is the default; API tokens remain available for self-hosted or advanced setup, while Base URL stays hidden unless GitHub/GitLab is marked self-hosted. OAuth returns can reopen Integrations through `?settings=integrations`. Heavier resource/webhook management stays out of the first pass.

## Design

See [DESIGN.md](DESIGN.md). The shell uses the landing-page palette: near-black dark canvas, Linear lavender primary, green pass states, amber warnings, red failures, and quiet hairline borders.

## Governance Agent

`src/components/agent/governance-agent.tsx` uses assistant-ui primitives.
`VITE_LANGGRAPH_API_URL` enables `@assistant-ui/react-langgraph` against the
`governance` graph only after a successful configured health response. Missing
configuration, a failed health request, or `configured: false` hides the chat
surface; there is no product fallback or placeholder setup UI.

In Vite dev, prefer `VITE_LANGGRAPH_API_URL=/api/agents` with
`SPECGATE_PROXY_TARGET` pointing at the appliance gateway. For raw-service mode,
leave it unset and point `LANGGRAPH_PROXY_TARGET` at the running agents service.
Direct cross-origin URLs also work when the agents server serves CORS correctly,
but the proxy path avoids stale port conflicts.

The UI does not expose ephemeral thread history or thread-management controls.
Switching workspaces remounts the runtime so old state and in-flight responses
are not reused. New conversations create a workspace-tagged LangGraph thread
directly; there is no separate title or sidebar-index API.

The composer offers only three accurate prompts: **Artifact summary**,
**Readiness results**, and **Knowledge search**. There is no `@` entity picker.
Governance chat is diagnostic and read-only; durable actions remain in their
Doc Registry or CLI workflows.
Press Enter to send a governance-agent message; use Shift+Enter for a newline. The composer shows Stop while a response is running.
