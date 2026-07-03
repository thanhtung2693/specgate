# Architecture

SpecGate is a monorepo with three modules brought up together by Docker Compose.

## Modules

| Module | Stack | Role |
|---|---|---|
| `app/doc-registry/` | Go, Postgres | Artifact storage, versioning, lifecycle, MCP + REST API |
| `app/agents/` | Python, LangGraph | Governance compute — gates, delivery review, governance chat |
| `app/ui/` | Vite + React + TypeScript | Operator console |

### app/doc-registry

The governance gateway and durable store. Handles artifact versioning and lifecycle enforcement, governance profiles and skills registries, Context Pack compilation, knowledge search (vector embeddings), webhook intake (Linear, GitLab, GitHub), the REST API the `specgate` CLI drives, and an MCP tool catalog for the internal governance-ops agent (`artifact_read_bundle`, `artifact_create`, `resolve_work_item`, `list_work_items`, `run_llm_gates`, `trigger_delivery_review`, `skills`, …). Coding IDE agents use the `specgate` CLI, not MCP. The readiness gate trigger delegates compute to agents via `AGENTS_BASE_URL`; if agents is unavailable the endpoint degrades gracefully.

### app/agents

Headless HTTP service and LangGraph graph. Exposes a single governance-chat graph (one node, system model, governance ops as tools) plus FastAPI routes for: readiness gates (LLM judges + completeness check, honoring the artifact's profile snapshot), delivery review, route classification, Context Pack generation, quick work-item creation, and thread-title classification. No sub-agents, no HITL, no drafting.

### app/ui

Operator console (experimental, source-build surface). Work board, reviews (delivery verdicts, gate failures, artifact-update proposals with approve/reject), artifact browser with gate transparency (catalog descriptions, evidence disclosure), advisory governance chat, and settings (general, models, governance skills + policy catalog, plugins, integrations). Authoring and implementation stay CLI-first.

## Boundaries

- **Doc Registry** is passive storage + governance gateway; it does not orchestrate runs.
- **Agents** is compute-only; called by doc-registry's readiness/gate endpoints and by the UI over HTTP.
- **UI** is the human control plane; it reads artifact state from doc-registry and triggers agents.
- No module couples to another's internal storage.

## Data flow

```
IDE agent  ──CLI publish──►  doc-registry  ──readiness (on demand)──►  agents
                                   │
                         human review + approve
                                   │
                         Context Pack compiled
                                   │
IDE agent  ◄──CLI context──────────┘

IDE agent  ──CLI delivery report/submit──►  doc-registry  ──delivery review──►  agents
                                                  │                               │
git/tracker  ──webhooks: status mirror +──────────┘                    verdict ──►│
              corroborating evidence                                              │
                                              human reads verdict (UI/CLI) ◄──────┘
```

Delivery review verdicts and gate runs persist with evaluator origin
(`platform` or `ide_agent`); a failed verdict carries its outstanding criteria
into the next Context Pack.

## Shared conventions

- API contracts are explicit when data crosses module boundaries.
- Versioned artifact IDs and status enums are preferred over free-form labels.
- `feature_id` is the stable cross-module key.
- Migrations are authoritative; AutoMigrate is not used.
