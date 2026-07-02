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
DOC_REGISTRY_PROXY_TARGET=http://127.0.0.1:8080
LANGGRAPH_PROXY_TARGET=http://127.0.0.1:2024
```

The browser-facing `VITE_*` URLs use Vite same-origin proxy paths in development. Override the proxy targets when your local compose ports differ, for example when another stack already owns `8080` or `2024`.

When `VITE_DOC_REGISTRY_URL` is unset, Work, Reviews, and Artifacts show empty setup-oriented placeholders instead of bundled sample rows. Configure the registry URL, then create or seed work through the CLI to populate those surfaces. When the URL is configured but the registry is unreachable or empty, live-mode surfaces show explicit loading, empty, or error placeholders instead of bundled sample rows, diffs, document bodies, or acceptance criteria.

When `VITE_LANGGRAPH_API_URL` is unset, the governance agent uses the deterministic local adapter. Set it to a running LangGraph API to use the `governance` graph through `@assistant-ui/react-langgraph`.
The same URL is used for governance custom routes such as quick Context Pack creation from the Work detail Handoff tab.

## Scripts

| Command | Purpose |
| --- | --- |
| `npm run dev` | Start the Vite dev server |
| `npm run lint` | Run oxlint |
| `npm run test` | Run Vitest component tests |
| `npm run build` | Type-check and build production assets |
| `npm run preview` | Preview a production build |
| `npm run docker:build` | Build the production nginx image as `specgate-ui:latest` |
| `npm run docker:run` | Run the image on [http://localhost:3000](http://localhost:3000) |

The production build keeps Mermaid in a separate lazy-loaded vendor chunk for document previews. The Vite chunk-size warning limit is set above that expected Mermaid payload so new warnings point to unexpected app-bundle growth.

CI runs these in [`.github/workflows/ui.yml`](../../.github/workflows/ui.yml):
`npm ci` then `npm run lint`, `npm run build`, and `npm run test` on Node 26,
path-filtered to `app/ui/**`.

## Docker

The production image is built from the monorepo root with [`../../docker/Dockerfile.ui`](../../docker/Dockerfile.ui). It compiles the Vite bundle and serves it through nginx with SPA route fallback, so deep links like `/work/SG-155` load correctly. In release Compose, nginx also proxies `/api/doc-registry` to the Doc Registry service and `/api/agents` to the agents service.

From this folder:

```bash
npm run docker:build
npm run docker:run
```

The full local stack already includes the UI service:

```bash
docker compose up --build ui
```

Vite `VITE_*` values are baked into the production image at build time. The release image uses `app/ui/.env.production`, which points the browser at the same-origin nginx proxy paths.

## Routes

`/` redirects to `/work`.

Shell routes:

- `/work`
- `/work/:itemKey`
- `/reviews`
- `/artifacts`

The browser title and sidebar product title are `SpecGate (Experimental)`, and the favicon uses the shared SpecGate logo asset from `public/logo.svg`.

Settings is an honest status/configuration modal opened from the sidebar footer, not a standalone page. Plugin install commands can be copied, server-side and embedding model settings save through Doc Registry `/settings`, and workspace CLI commands can be copied for persistent selection. Knowledge (Experimental) can list and add Governance Knowledge when embeddings are configured. Integrations (Experimental) can add GitHub, GitLab, or Linear through Doc Registry and store a write-only provider API token; hosted OAuth and SaaS token setup hide Base URL unless GitHub/GitLab is marked self-hosted. OAuth returns can reopen Integrations through `?settings=integrations`. Heavier resource/webhook management stays out of the first pass.

## Design

See [DESIGN.md](DESIGN.md). The shell uses the landing-page palette: near-black dark canvas, Linear lavender primary, green pass states, amber warnings, red failures, and quiet hairline borders.

## Governance Agent

`src/components/agent/governance-agent.tsx` uses assistant-ui primitives. Runtime selection lives in `src/components/agent/governance-runtime.tsx`: `VITE_LANGGRAPH_API_URL` enables `@assistant-ui/react-langgraph` against the `governance` graph, while the deterministic local adapter remains the fallback for layout work.

In Vite dev, prefer `VITE_LANGGRAPH_API_URL=/api/agents` with `LANGGRAPH_PROXY_TARGET` pointing at the running agents service. Direct cross-origin URLs also work when the agents server serves CORS correctly, but the proxy path avoids stale port conflicts.

The thread page lists SpecGate-created governance threads with readable titles and relative activity times. Titles are generated by the agents service custom route `POST /governance/threads/{thread_id}/title` and stored in LangGraph thread metadata; raw thread ids and runtime names are not shown in the UI.

The composer supports `@` context insertion for work items, artifacts, and skills, plus `/` governance prompts for handoff, gates, and evidence.
Press Enter to send a governance-agent message; use Shift+Enter for a newline. The composer shows Stop while a response is running.
