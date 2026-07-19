# Architecture

Use this explanation when changing a SpecGate module or reviewing a
cross-module design. It describes runtime responsibilities, sources of truth,
and trust boundaries; it is not an installation guide.

SpecGate is a local-first governance layer between approved intent and coding
agents. It keeps product scope, artifact versions, Context Packs, delivery
evidence, and review verdicts in one place while users keep their existing IDE,
tracker, and git workflow.

## Runtime shapes

SpecGate has two product modes. Local mode is a CLI-only workflow:

```text
Developer / IDE agent
  -> specgate CLI
     -> user-private SQLite store
     -> immutable artifact documents in that store
```

Local mode has no server, browser UI, external model, Redis, Postgres, or blob
service. IDE agents execute the semantic gate tasks that a configured model may
execute in Full mode.

Full mode uses the service boundaries below:

```text
Developer / IDE agent
  -> specgate CLI
     -> Doc Registry REST API
        -> Postgres
        -> local blob storage or S3/MinIO
        -> optional Redis queue

Browser UI
  -> Doc Registry REST API
  -> Agents service for governance chat and model-backed workflows

Agents service
  -> Doc Registry REST
  -> configured model provider
```

The single-container local appliance packages Full mode for easy installation:

```text
localhost:<SPECGATE_PORT>
  -> gateway
     -> /                     UI
     -> /api/doc-registry/   Doc Registry
     -> /api/agents/         Agents
     -> /integrations/oauth-callback
     -> /integrations/{integration}/resources/{resource}/{provider}/webhook
                              provider-only Doc Registry callbacks

specgate container
  -> Postgres
  -> Doc Registry
  -> Agents
  -> gateway + static UI
  -> specgate-data:/data
```

It publishes one port and uses one named volume. Postgres and internal APIs bind
inside the container. Redis and MinIO are not part of the local appliance.

Self-hosted and cloud deployments may run the same Full-mode services as
separate containers. `specgate init` is the explicit mode boundary: `local`
creates the private SQLite workflow, while `full` installs or connects to the
service-backed workflow.

## Modules

| Module | Stack | Responsibility |
| --- | --- | --- |
| `app/cli` | Go / Cobra | install, setup, local config, workspace binding, work/artifact/delivery commands, IDE plugin install |
| `app/doc-registry` | Go / Huma / Postgres | durable state, artifacts, workboard, Knowledge, settings, integrations, REST |
| `app/agents` | Python / LangGraph | model-backed governance operations: readiness, delivery review, quick-work acceptance criteria, governance chat |
| `app/ui` | Vite / React | human review, artifact inspection, work queues, settings, team/workspace views, governance chat |
| `plugins` / `agentpackages` | Markdown + manifests | IDE skills, hooks, and installable plugin package assets |

## Source of Truth

In Full mode, Doc Registry is the durable source of truth. It owns:

- artifact lifecycle and file metadata;
- work items and acceptance criteria;
- gate runs and delivery review;
- Knowledge metadata and retrieval;
- integration signals;
- settings and encrypted secrets.

In Local mode, the private SQLite store owns artifacts, work, gates, evidence,
and attribution for that installation.

The agents service may judge, classify, summarize, or propose, but writes
through Doc Registry APIs. The UI renders and reviews state; it does not keep
hidden sample data or alternate workflow state.

## Trust Boundaries

SpecGate alpha assumes a trusted local or private network.

- Doc Registry has no public HTTP auth layer.
- Local users and workspaces are attribution and filtering, not RBAC.
- Settings secrets are encrypted at rest with `SETTINGS_ENCRYPTION_KEY`.
- IDE-agent output is evidence, not approval.
- Human approval and deterministic checks outrank model judgment.

See [Trust and security](../using-specgate/concepts/trust-and-security.md).

## Workflow

```text
create or import work
-> shape artifact package
-> run readiness gates
-> approve artifact
-> assemble Context Pack
-> coding agent implements
-> submit delivery evidence
-> delivery review records verdict
-> human approves/rejects when needed
```

Quick work can skip the full artifact package when the change is small and
acceptance criteria are enough for a Context Pack. Local quick work stores the
same artifact-free immutable handoff in SQLite and requires explicit criteria;
Full mode may draft criteria through the platform model. Full work keeps the
artifact-package loop.

## Storage and Queues

Full mode requires Postgres. It stores metadata, workboard state, Knowledge
chunks, gate history, settings, and integrations. Local mode uses SQLite and
does not start Postgres.

Blob storage stores document contents and uploads. The appliance uses the
`specgate-data` Docker volume; its deployment environment can point storage at
an S3-compatible service when needed.

Redis is optional. It backs async Knowledge ingest and webhook processing when
enabled; otherwise local/sync processing is used.

## Interfaces

- In Full mode, the CLI connects through `/api/doc-registry`, then uses
  `/api/v1/*`. In Local mode, it opens only its selected SQLite store.
- UI uses Doc Registry's app routes and the agents service.
- Agents use Doc Registry REST.
- IDE plugins install focused skills/hooks/rules and call the CLI.
- In Full mode, GitHub/GitLab selected resources provide managed-webhook
  repository observation, while an optional selected Linear team receives an
  explicit work handoff. Repository observation compares the submitted PR/MR
  head with the latest completion receipt; Linear never decides delivery.

## Related

- [Contracts](contracts.md)
- [Data model](data-model.md)
- [Operate SpecGate](../using-specgate/guides/operate-specgate.md)
- [Testing strategy](testing.md)
