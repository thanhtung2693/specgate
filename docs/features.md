# SpecGate feature status

SpecGate runs the full governed delivery loop with no LLM key required. All
product capabilities in this repository are available in the product. Some
capabilities are still experimental and opt-in because their interfaces, setup,
and operational defaults are evolving.

## Stable

These capabilities are the supported alpha core:

- artifacts, governance levels, the policy and gate framework;
- evidence, gate tasks, and trust stamping;
- deterministic gates and LLM readiness gates, run either on a configured
  server-side model or by your IDE coding agent;
- stale-handoff warnings;
- the full `specgate` CLI and plugins / skills;
- delivery review from coding-agent acceptance-criteria claims, with optional
  LLM judgment when a server-side model is configured.

## Experimental

These features are available today. Turn them on when you need them and expect
rough edges while the contracts mature:

| Feature | Enable it | Notes |
|---|---|---|
| Integrations (GitHub / GitLab / Linear) + inbound webhooks | configure an integration (OAuth/credentials) | tracker handoff, PR/MR/CI evidence signals |
| Knowledge search + vector DB + embeddings | `KNOWLEDGE_DRIVER=pgvector` + an embedding provider | semantic search over governed knowledge |
| Governance-ops chat agent | run the agents service via LangGraph (`langgraph.json`) instead of the uvicorn webapp | needs a server-side model; see [`../app/agents/README.md`](../app/agents/README.md) |

Feedback on experimental features is especially welcome as they mature.

## Licensing

The repository is Apache-2.0. See [`../LICENSING.md`](../LICENSING.md).
