# Trust and security

SpecGate v0.1 is designed for local evaluation and trusted self-hosted
networks. Do not expose the Doc Registry API, agents service, or web UI directly
to the public internet without additional access control.

## Current posture

The v0.1 stack prioritizes local workflow validation:

- CLI-first setup;
- trusted network assumption;
- no full end-user authentication layer;
- local user and global/project workspace selection for attribution and
  filtering;
- optional API keys for model providers and external integrations.

Treat every service endpoint as internal unless you add your own boundary.

## Trusted-network boundary

Keep default services bound to your machine or private network. If you put
SpecGate behind a reverse proxy, add:

- TLS;
- authentication;
- request size limits;
- log redaction;
- secret management;
- webhook signature verification.

Do not publish a local evaluation stack directly on the public internet.

## CLI and REST access

The CLI stores a server URL and local user/workspace selection. This selection
is not login. It controls attribution and default filtering. Workspace
selection can be global or bound to a local Git checkout; both forms live in
the user CLI config and do not grant access by themselves.

REST endpoints in the v0.1 stack are intended for trusted local use. Production
deployments should place them behind an auth gateway or service mesh.

Do not commit model keys, webhook secrets, or OAuth credentials.

## Evidence and integrations

Delivery evidence can come from:

- agent-submitted completion reports;
- user-cited or externally supplied test and CI output;
- selected-resource PR/MR observations and optional Linear signals;
- human notes.

SpecGate records evidence provenance. Policies can require corroborated evidence
for stricter delivery review. It does not ingest provider CI state or create a
delivery-assurance source from it.

## Data sensitivity

Artifact/spec data may include product plans, source paths, issue details, and
implementation evidence. Treat the Full appliance's managed `specgate-data`
volume with the same care as source code and tracker data when backing up or
purging it.

The artifact event log is tamper-evident: every `artifact_events` row hash-links
to its predecessor, and `specgate audit <ref> --verify` recomputes the chain and
reports the first broken link. This evidences direct database edits; it does not
detect deletion of the newest events (no external anchor) and it does not defend
against a compromised server binary.

Encryption at rest is infra-level in the self-hosted stack: use Postgres with
encrypted storage, S3-compatible server-side encryption, and OS-level disk
encryption for the Full appliance volume. Settings
secrets (provider API keys) are additionally application-encrypted via
`SETTINGS_ENCRYPTION_KEY`. Application-level, per-workspace artifact encryption
is deferred — see
[the artifact-encryption ADR](../../contributing/adr/2026-07-10-artifact-encryption-at-rest.md)
for the decision, constraints (presigned URLs, embeddings), and revisit
trigger.

## Safe deployment checklist

- Keep services private.
- Use TLS and authentication at the edge.
- Store secrets outside git.
- Back up the appliance and retain `SETTINGS_ENCRYPTION_KEY`.
- Verify webhook signatures.
- Redact logs before sharing.
- Run `specgate doctor` after upgrades.

## Related

- [Operate SpecGate](../guides/operate-specgate.md)
- [Configuration reference](../reference/configuration.md)
- [Evidence reference](../reference/evidence.md)
