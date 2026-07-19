# ADR: Artifact encryption at rest for the hosted tier

**Date:** 2026-07-10
**Status:** Accepted (direction); implementation deferred until a hosted tier exists

## Context

Specs and Context Packs are company-sensitive IP. In today's self-hosted
deployment the at-rest story is infra-level: managed Postgres encrypts its
volumes, S3 encrypts by default (SSE-S3), MinIO/local disks are the operator's
responsibility. That is sufficient while the operator owns the data.

A future hosted (multi-tenant SaaS) SpecGate changes the threat model: tenants
must be protected from provider-infrastructure breaches, leaked backups or
bucket misconfiguration, and cross-tenant storage bugs. Infra-level encryption
does not cover those — the application decrypts everything transparently.

End-to-end encryption (server never reads content) is ruled out permanently:
the server must read spec content to run LLM readiness gates, render Context
Packs, run delivery review, and embed Knowledge. Confidentiality from the
LLM providers is a model-choice/self-hosting concern, not a storage concern —
no at-rest scheme changes what gate runs send to a provider.

## Decision

1. **Self-hosted (now):** at-rest encryption stays infra-level. Documented in
   `docs/using-specgate/concepts/trust-and-security.md`; no application code.
2. **Hosted tier (future):** per-workspace **envelope encryption** applied at
   the `artifact.ObjectStore` boundary — a wrapping store that encrypts on
   `PutObject` / decrypts on `GetObject` with a per-workspace data key, itself
   wrapped by a KMS-managed master key (BYOK-capable). The settings crypto
   (`SETTINGS_ENCRYPTION_KEY`, AES envelope) is the in-repo precedent for the
   pattern. Sensitive Postgres columns (artifact file content is not stored in
   Postgres; chat threads and knowledge sources would be evaluated case by
   case) follow the same per-workspace key.

## Constraints the design must honor (why this ADR exists)

- **`ObjectStore` stays the single blob choke point.** All artifact reads and
  writes go through its 4 methods; new features must not bypass it, or the
  future wrapper will not cover them.
- **Presigned URLs are incompatible with app-level encryption.** A presigned
  GET returns ciphertext under an encrypting wrapper. The hosted tier must
  either (a) serve downloads through the app, or (b) use SSE-KMS with
  per-tenant KMS grants instead of app-level crypto for presign-dependent
  paths. Features added between now and then should prefer app-served content
  over new presign dependencies.
- **Embeddings leak.** pgvector rows are derived from plaintext and stored
  unencrypted; vectors carry semantic information. The hosted tier either
  scopes embeddings under the same per-workspace key treatment or documents
  the residual exposure.
- **Workspace is the tenant key-scoping unit.** Per-workspace data keys align
  with the existing `workspaces` model; nothing should introduce cross-
  workspace shared blobs.

## Alternatives considered

- **App-level encryption now (self-hosted):** rejected — the operator already
  owns the disk; it adds key management for no covered threat and complicates
  local dev.
- **E2EE:** rejected — incompatible with LLM gates, Context Pack rendering,
  and Knowledge search (the product).
- **Column-level Postgres encryption for artifact metadata:** deferred —
  metadata (titles, versions, statuses) is low-sensitivity relative to file
  content; revisit with the hosted tier.

## Revisit trigger

Design work on a hosted/multi-tenant offering, or the first customer
requirement for BYOK / SOC 2 evidence of tenant-scoped encryption.
