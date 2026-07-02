# Trust and security

SpecGate currently assumes services run inside a trusted network. Several
focused credentials exist, but they do not provide full product authentication
or RBAC.

## Current Posture

Doc Registry’s general REST and UI-facing endpoints do not enforce user
authentication. The deployment boundary—local machine, private network, VPN,
VPC, or service mesh—is the primary access control.

This keeps setup simple, but operators must treat the service as
internal infrastructure.

> Do not expose Doc Registry’s general HTTP surface directly to the public
> internet. Evidence actor keys and the MCP bearer key do not turn the whole
> product into an authenticated multi-user service.

## Trusted-network boundary

Safe deployments should:

- bind services to private interfaces where possible;
- restrict inbound traffic with host, container, or network policy;
- expose only routes needed by external systems;
- terminate TLS at a trusted reverse proxy;
- protect the Web UI with network or proxy controls;
- keep settings, token-management, and admin endpoints private.

The reference compose stack binds the Postgres port (`5432`) to `127.0.0.1` so it
is not reachable off-host; keep that binding (or remove the host port) on any
shared machine.

> **Unmasked-secrets header.** `GET /settings` normally masks secret values, but
> a request carrying `X-SpecGate-Internal-Agent: governance` receives them
> **unmasked** — this is how the agents service reads provider keys. There is no
> token behind the header; it relies entirely on the trusted-network boundary.
> Never expose the settings routes to untrusted callers. `PUT /settings` likewise
> has no auth and can overwrite keys.

## CLI and REST access

The CLI calls versioned REST facades. Those routes inherit the trusted-network
posture.

Point the CLI only at a registry you trust:

```bash
specgate config set server https://specgate.internal.example
specgate doctor
```

The CLI configuration stores the server URL and selected local user/workspace,
not general user credentials. Local identity supports attribution and selection;
it does not authenticate requests or authorize operations.

## MCP bearer access

The governance-ops MCP stream requires a bearer key. SpecGate generates an MCP
key on first startup unless an environment override is supplied.

That key protects the MCP stream, not the general HTTP surface. Internal
endpoints that display or rotate the MCP key also rely on the trusted network
and must not be exposed publicly.

If a reverse proxy exposes MCP outside the internal network, expose only the
required MCP stream route—not settings or token-management routes.

## Evidence actor API keys

Evidence submissions may include an actor API key. SpecGate maps the key to a
registered actor type and stamps the resulting trust level server-side.

Actor types include:

- IDE agent;
- external verifier;
- runtime monitor;
- human.

Administration requires `ADMIN_SECRET` and is separate from normal product
access. Evidence keys:

- authenticate the evidence producer;
- support stronger provenance and independent-review checks;
- can be revoked.

They do not authenticate UI sessions or authorize general artifact operations.

## Webhook authentication

Inbound provider webhooks use provider-specific signatures or secrets:

- GitHub uses HMAC signatures;
- GitLab uses its configured signing token;
- Linear uses its webhook-signing mechanism.

SpecGate verifies the raw request before treating the event as delivery
evidence. Duplicate deliveries are recorded and ignored rather than applied
twice.

## What is not enforced

The current release does not provide:

- user login and session management;
- workspace or tenant isolation;
- general endpoint RBAC;
- compliance-grade separation of duties;
- authoritative identity behind every `created_by`, `approved_by`, or
  `actor_kind` value.

Some actor fields are cooperative assertions used for workflow and audit
context. The backend applies limited policy guards, but the network and operator
remain part of the trust model.

## Safe deployment checklist

- [ ] Services are not directly reachable from the public internet.
- [ ] TLS and access controls exist at the reverse proxy or private network.
- [ ] `SETTINGS_ENCRYPTION_KEY` is strong, backed up, and not committed.
- [ ] Provider API keys and OAuth secrets stay in environment or encrypted settings.
- [ ] `ADMIN_SECRET` is set only when evidence actor administration is needed.
- [ ] MCP key-management routes remain private.
- [ ] Webhook secrets are unique and rotated when exposure is suspected.
- [ ] Persistent data is backed up before upgrades or destructive operations.
- [ ] Logs and support bundles are checked for sensitive URLs or credentials.

## Continue

- [Operate SpecGate](../guides/operate-specgate.md)
- [Configuration reference](../reference/configuration.md)
