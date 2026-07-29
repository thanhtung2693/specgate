# Trust and security

SpecGate v0.1 is designed for local evaluation and trusted self-hosted
networks. Do not expose the Doc Registry API, agents service, or web UI directly
to the public internet without additional access control.

## Current posture

The v0.1 stack prioritizes local workflow validation:

- CLI-first setup;
- trusted network assumption by default, with optional gateway credentials for a
  shared appliance (see below);
- no identity provider, sessions, or per-user roles;
- local user and global/project workspace selection for attribution and
  filtering;
- optional API keys for model providers and external integrations.

Treat every service endpoint as internal unless you add your own boundary.

## Sharing one appliance with a few developers

A default appliance publishes one port on `127.0.0.1` and trusts whoever reaches
it. That is right for one person on one machine. When two or three developers
share an appliance, issue each of them a credential:

```bash
specgate workspace credential mai
```

The appliance generates the secret and prints it once. On that developer's
machine:

```bash
specgate config credential mai
```

Issuing the first credential turns authentication on for everything the gateway
serves — the API, and the browser, which prompts once and remembers. The gateway
then tells SpecGate who the caller is, so the name recorded on an approval or an
acceptance is the one that authenticated rather than one the caller typed.
Revoking the last credential returns the appliance to trusting its network.

Two limits worth knowing before you rely on it:

- **A credential identifies a credential, not a person.** Hand yours to a
  teammate or paste it into an agent's configuration and the ledger will record
  your name for their decisions.
- **Credentials are reversible on the wire.** Beyond loopback, put the appliance
  on a private overlay network (Tailscale, WireGuard) or terminate TLS in front
  of it. For a small team the overlay is usually less work than certificates.

Local mode has no gateway and no credentials: one binary, one file, one machine.

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

Use encrypted storage and OS-level disk encryption for Full-appliance data.
Settings secrets (provider API keys) are additionally application-encrypted.
Application-level, per-workspace artifact encryption is not yet available.

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
