# SpecGate feature status

Use this reference to see which SpecGate capabilities are established and which
are newer. Both are usable; the newer group has had less real-world mileage, so
its interfaces are the more likely to change.

## Established paths

- CLI install, Local CLI initialization, and Full appliance initialization with
  `specgate init`.
- Local user and per-project workspace selection for attribution and filtering.
- One-command workspace overrides with `--workspace` or `SPECGATE_WORKSPACE`.
- Local and Full quick work item creation with acceptance criteria.
- Artifact publication, versioning, status, and Context Pack handoff.
- Full-mode automatic governance resolution across `light`, `standard`, and
  `enhanced`; Local artifact-backed work uses fixed Standard governance.
- Delivery report scaffolding, deterministic check bindings enforced in both
  Local and Full review, evidence-grounded citations, human delivery
  approve/reject, and `delivery submit`.
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
- Local resume packets and indexed reads of approved document snapshots.
- Optional Local verification contracts that pin reviewed `@check` commands
  before reporting; Local acceptance requires the exact `--review-id` from
  reviewed status. Pinned contracts cannot be exported to Full mode.
- `specgate stats` governance-value reporting from real gate and delivery
  history in both modes, including first-pass yield, pre/post-build governance
  signals, rework, and cycle time.
- Full-mode GitHub/GitLab **Repositories** with selected-resource managed
  webhooks and exact-head merged PR/MR observation.
- Optional Full-mode Linear **Work tracking** handoff to one selected team;
  direct IDE-agent Context Pack handoff remains available.

## Newer surfaces

- Web UI review, workflow scanning, settings, workspace members, and artifact
  inspection.
- Governance chat for advisory help around gates, artifacts, and delivery
  context.
- Full-mode workspace-scoped Knowledge upload, queueable ingest, embedding-backed search,
  citations, Context Pack Knowledge provenance, and
  linked-Knowledge freshness warnings.
- Platform-model readiness checks and delivery review.
- Committable delivery handoffs: `delivery handoff export` writes a review
  request into the repository and `delivery handoff show` renders it read-only
  without a workspace or server. Reviewer decisions are not carried in the file,
  and the bundle schema can still change.

These surfaces work today. They have had less team mileage than the established
paths, so their interfaces are the more likely to change.

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
