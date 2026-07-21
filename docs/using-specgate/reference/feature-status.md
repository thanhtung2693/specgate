# SpecGate feature status

Use this reference to decide which SpecGate capabilities are ready for cautious
v0.1 evaluation and which still need extra care. It does not promise
stability beyond the classifications below.

## Core v0.1 paths

- CLI install, Local CLI initialization, and Full appliance initialization with
  `specgate init`.
- Local user and per-project workspace selection for attribution and filtering.
- One-command workspace overrides with `--workspace` or `SPECGATE_WORKSPACE`.
- Local and Full quick work item creation with acceptance criteria.
- Artifact publication, versioning, status, and Context Pack handoff.
- Automatic governance resolution across the built-in `light`, `standard`, and
  `enhanced` tiers.
- Delivery report scaffolding, deterministic check bindings,
  evidence-grounded citations, human delivery approve/reject, and
  `delivery submit`.
- Delivery-trust readback separates evidence, assurance source, human decision,
  and recorded Git receipt; agent-reported evidence remains explicit whether or
  not a platform model reviews the submitted claims.
- Change facade: `change approve`, `change status`, `change submit`, `change
  accept`, and `change request-changes` work against existing artifact, work,
  and delivery records in Local and Full mode; they do not create a durable
  Change entity.
- Local and Full model-less semantic readiness through frozen IDE gate tasks;
  results are `agent_attested` and human approval remains separate.
- Workspace peer governance: local users/workspaces, workspace member readback,
  same-agent delivery-approval guard, completion-bound human decisions, and
  peer-reviewed delivery evidence.
- Embedded Codex, Claude Code, and Cursor plugin install in Local mode without
  a registry; the same IDE targets are available in Full mode.
- Safe uninstall that keeps data by default.
- `specgate stats` governance-value reporting from real gate and delivery
  history, including first-pass yield and pre/post-build governance signals.
- Full-mode GitHub/GitLab **Repositories** with selected-resource managed
  webhooks and exact-head merged PR/MR observation.
- Optional Full-mode Linear **Work tracking** handoff to one selected team;
  direct IDE-agent Context Pack handoff remains available.

## Experimental v0.1 foundations

- Web UI review, workflow scanning, settings, workspace members, and artifact
  inspection.
- Governance chat for advisory help around gates, artifacts, and delivery
  context.
- Full-mode workspace-scoped Knowledge upload, queueable ingest, embedding-backed search,
  citations, Context Pack Knowledge provenance, and
  linked-Knowledge freshness warnings.
- Platform-model readiness checks and delivery review.

These surfaces are usable in local evaluation, but they may change during
v0.1 and still need more real team use before they become stable
paths.

## Deferred from the Change facade

`change prepare` is not available. Agents use the artifact and gate commands to
prepare the exact snapshot and explicit work contract; `change approve` then
coordinates approval, canonical promotion, work creation, and Context Pack
verification.

## Not goals for v0.1

- Public multi-tenant hosting.
- Full end-user authentication and authorization.
- Replacement for CI, PR review, trackers, or authoring tools.
- Guaranteed model judgment without human review.

## Related

- [Quickstart](../quickstart.md)
- [Trust and security](../concepts/trust-and-security.md)
- [CLI reference](cli.md)
