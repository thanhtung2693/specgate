# Architecture decisions

These records explain decisions that constrain SpecGate implementation. Read
the relevant ADR before changing the affected architecture or contract.

- [Delivery-verdict trust model](2026-07-07-delivery-trust-model.md)
- [Completion-bound delivery acceptance](2026-07-19-completion-bound-delivery-acceptance.md)
- [Minimal team integration boundary](2026-07-20-minimal-team-integrations.md)
- [Governance Knowledge RAG alpha](2026-07-07-knowledge-rag-alpha.md)
- [Server as governance truth](2026-07-08-server-as-governance-truth.md)
- [Artifact encryption at rest](2026-07-10-artifact-encryption-at-rest.md)
- [Local mode as a parallel implementation](2026-07-29-local-mode-parallel-implementation.md) — proposed, awaiting a decision
- [Gateway-asserted identity for the shared appliance](2026-07-29-gateway-asserted-identity.md) — accepted and implemented

Add a new ADR when a decision changes system boundaries, trust assumptions,
data ownership, or a cross-module contract. Do not rewrite an accepted decision
to hide its history; supersede it explicitly.
