# Trust and security

SpecGate alpha is designed for local evaluation and trusted self-hosted
networks. Do not expose the Doc Registry API, agents service, or web UI directly
to the public internet without additional access control.

## Current posture

The alpha stack prioritizes local workflow validation:

- CLI-first setup;
- trusted network assumption;
- no full end-user authentication layer;
- local user/workspace selection for attribution and filtering;
- optional API keys for MCP, evidence actors, providers, and integrations.

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
is not login. It controls attribution and default filtering.

REST endpoints in the alpha stack are intended for trusted local use. A future
production deployment should place them behind an auth gateway or service mesh.

## MCP bearer access

Doc Registry can expose MCP tools. Set `MCP_API_KEY` when you need a stable
token. If unset, Doc Registry mints a random key at startup.

Do not commit MCP keys, model keys, webhook secrets, or OAuth credentials.

## Evidence and integrations

Delivery evidence can come from:

- agent-submitted completion reports;
- CI or webhook events;
- tracker or PR integrations;
- human notes.

SpecGate records evidence provenance. Policies can require corroborated evidence
for stricter delivery review.

## Data sensitivity

Artifact/spec data may include product plans, source paths, issue details, and
implementation evidence. In the default local stack:

| Storage | Sensitive contents |
|---|---|
| Postgres | metadata, settings, work items, evidence, gate history |
| `doc-registry-data` | artifact/spec document contents |

Back up and purge these stores with the same care you use for source code and
tracker data.

## Safe deployment checklist

- Keep services private.
- Use TLS and authentication at the edge.
- Store secrets outside git.
- Back up Postgres, blob storage, and `SETTINGS_ENCRYPTION_KEY`.
- Verify webhook signatures.
- Redact logs before sharing.
- Run `specgate doctor` after upgrades.

## Related

- [Operate SpecGate](../guides/operate-specgate.md)
- [Configuration reference](../reference/configuration.md)
- [Evidence reference](../reference/evidence.md)
