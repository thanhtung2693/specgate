# Architecture

SpecGate is a monorepo with three modules brought up together by Docker Compose.

## Modules

| Module | Stack | Role |
|---|---|---|
| `app/doc-registry/` | Go, Postgres | Artifact storage, versioning, lifecycle, MCP + REST API |
| `app/agents/` | Python, LangGraph | Governance compute — gates, delivery review, reconciliation |
| `app/ui/` | Vite + React + TypeScript | Operator console |

### app/doc-registry

The governance gateway and durable store. Handles artifact versioning and lifecycle enforcement, governance profiles and skills registries, Context Pack compilation, knowledge search (vector embeddings), webhook intake (Linear, GitLab, GitHub), the REST API the `specgate` CLI drives, and an MCP tool catalog for the internal governance-ops agent (`artifact_read_bundle`, `artifact_create`, `resolve_work_item`, `list_work_items`, `run_llm_gates`, `trigger_delivery_review`, `skills`, …). Coding IDE agents use the `specgate` CLI, not MCP. The readiness gate trigger delegates compute to agents via `AGENTS_BASE_URL`; if agents is unavailable the endpoint degrades gracefully.

### app/agents

Headless HTTP service and LangGraph graph. Exposes a single governance-chat graph (one node, system model, governance ops as tools) plus FastAPI routes for: readiness gates (6 LLM judges + completeness check), delivery review, reconciliation / artifact-patch proposals, intent classifier, lifecycle suggestions, feature summaries. No sub-agents, no HITL, no drafting.

### app/ui

Operator console (experimental, source-build surface). Work board, reviews (delivery verdicts, gate failures, artifact-update proposals), artifact browser, advisory governance chat, and settings (server-side + embedding model, integrations, skills, governance profiles, knowledge). Authoring and implementation stay CLI-first.

## Boundaries

- **Doc Registry** is passive storage + governance gateway; it does not orchestrate runs.
- **Agents** is compute-only; called by doc-registry's readiness/gate endpoints and by the UI over HTTP.
- **UI** is the human control plane; it reads artifact state from doc-registry and triggers agents.
- No module couples to another's internal storage.

## Data flow

```
IDE agent  ──CLI publish──►  doc-registry  ──version + gate trigger──►  agents
                                   │
                         human review + approve
                                   │
                         Context Pack compiled
                                   │
IDE agent  ◄──CLI context──────────┘

IDE agent  ──build──►  git/tracker  ──webhooks──►  doc-registry  ──reconcile──►  agents
                                                                                      │
                                                                         proposal ──► UI ──► human review
```

## Shared conventions

- API contracts are explicit when data crosses module boundaries.
- Versioned artifact IDs and status enums are preferred over free-form labels.
- `feature_id` is the stable cross-module key.
- Migrations are authoritative; AutoMigrate is not used.
