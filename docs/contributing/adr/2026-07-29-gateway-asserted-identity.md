# ADR: Gateway-asserted identity for the shared appliance

## Status

Accepted and implemented — 2026-07-29. Supersedes nothing. Constrains
[`app/doc-registry/AGENTS.md`](../../../app/doc-registry/AGENTS.md) §7 and
[Trust and security](../../using-specgate/concepts/trust-and-security.md).

## Context

The product's central claim is that a named human approved an exact version and
accepted an exact completion. Two separation-of-duties rules already back it,
and both are enforced in both modes:

- a coding agent cannot peer-review its own completion
  (`app/cli/internal/local/delivery.go`, bound to the completion event and its
  Git receipt);
- whoever filed the completion cannot approve it
  (`internal/governanceops/delivery_decision.go`, approve-only — a reporter may
  reject its own work).

Both compare identity **strings**. The Full-mode code says so in a comment: the
match is "case-insensitive and best-effort … mainly guards the case where the
same identity string filed and approved". The actor arrives from the caller's
own configuration (`currentActor` reads the local config username), and the
service accepts any string. So the rules stop an honest mistake and a lazy
agent; they do not stop anyone who types a different name.

The deployment reality is narrower than the risk sounds. The released appliance
is a single container publishing one port, and the compose file binds it to
`127.0.0.1:${SPECGATE_PORT:-3000}`. Its nginx is the sole ingress; Postgres, the
object store, the agents service, and Doc Registry are reachable only on the
internal network, and nginx already strips the privileged
`X-SpecGate-Internal-Agent` header from client traffic — a release test asserts
this for both gateway configs. On a default install the boundary is the machine,
which is exactly right for the solo wedge and needs no credential ceremony.

The gap opens at one specific moment: someone edits the compose binding to share
the appliance with teammates. Today that turns every governance write into an
unauthenticated operation for anyone who can reach the port, with no warning and
no recipe — while the docs correctly say "add authentication" without saying how.

The concrete target is small and known: **two or three developers on one
self-hosted appliance inside a private network.** That size decides the design.
Per-user credentials are a file with three lines, so the operational cost of
real per-user identity is close to the cost of one shared secret — while the
benefit is not close at all, because a shared secret leaves every name in the
ledger self-declared among people who all hold it. Those developers also use the
web UI daily, so the browser is a primary path, not an edge case.

Members are already a first-class entity. Doc Registry has local users and
workspace members with a readback API (`ListWorkspaceMembers`), so "who belongs
to this appliance" is answered by the database today. Any credential store that
lives somewhere else creates a second answer to the same question.

One more fact decides where the actor comes from. Today the actor travels in the
**request body** (`approved_by`, `decided_by`), and a code comment states the
reason: "no HTTP auth; supplied in the request". Authenticating the transport
while still trusting a body field would be decoration: the caller would prove
who they are and then name someone else.

## Constraints

- Doc Registry must not grow JWT/RBAC middleware (module rule); the enforcement
  point has to be the gateway.
- Local mode gets nothing: one binary, one SQLite file, one machine — solo needs
  no authentication, and the loopback default must stay frictionless.
- No OAuth, no identity provider, no invented passwords, and no session layer
  built into the product.
- Credentials keep the existing handling rule: mode `0600`, never printed, status
  reports set/not-set only.

## Options

**A. Do nothing; keep documenting the trusted-network assumption.**
Free and honest. Leaves the shared-appliance case with no story, which is the
case the team claim depends on.

**B. One shared appliance token, checked by nginx.** Cheapest to operate and
enough to keep strangers out, but every name in the ledger stays self-declared
among the people who all hold the secret, so it does not answer the question this
work exists to answer. It also cannot gate the browser, which holds no bearer
token without a login page to put it there.

**C. Per-user credentials in a gateway credential file (htpasswd).** Real
per-user identity, and the browser is covered because its credential manager
stores the credential after one prompt. Rejected after building it: the file is a
second source of truth for membership beside the database, and enabling it is
fragile — verified empirically that nginx answers **401 to every request** when
`auth_basic_user_file` names a missing file, so shipping the directives
unconditionally would lock out every existing loopback appliance, and avoiding
that needs generated configuration in a released container.

**D. Doc Registry authenticates its own API.** Puts credentials in the database
where members already live, needs no generated configuration, and a `401` with
`WWW-Authenticate` still makes the browser prompt. But it gates only Doc
Registry: the agents service under `/api/agents/` and the UI shell stay open, so
the appliance would be half-covered.

**E. nginx delegates authentication to Doc Registry via `auth_request`
(chosen).** The gateway keeps enforcing for everything it serves, while the
credential store stays in the database with the members. nginx calls an internal
Doc Registry endpoint per request, and takes the authenticated identity from that
response.

**F. Browser pairing code (Ted's idea).** The CLI prints a code, the developer
types it into the UI, and the browser gets a session. A real pattern with better
UX than a credential prompt, but it layers on top of E rather than replacing it:
the code proves the person also controls an already-authenticated CLI, so the CLI
still needs a credential first. It costs a session store, an issue/verify/expiry
path, and cookie handling. Since the browser already remembers the credential
under E, this buys convenience, not capability. Recorded as the upgrade to reach
for if the prompt proves annoying.

**G. mTLS or an identity provider.** Stronger, and far more ceremony than three
developers on a private network will accept.

## Decision

Adopt **E**. Verified against the real gateway configuration before writing it
down: with `auth_request` pointing at a mock verifier, an unauthenticated request
is refused `401`, a valid credential reaches the backend, and a request that
presents a valid credential **plus** its own `X-SpecGate-User: boss` header
arrives at the backend as the authenticated user, not as `boss`.

1. **Off by default.** With no member credentials configured, the verifier
   reports that authentication is not configured and nginx passes the request
   through. A default loopback appliance keeps working with no prompt, and Local
   mode is untouched.
2. **Credentials live in the database with the members**, hashed with bcrypt —
   verified that the appliance's nginx accepts bcrypt, apr1, and SHA-512 crypt
   entries, so the strongest of the three is available. There is no credential
   file and no second membership list.
3. **nginx delegates and owns identity.** An internal `auth_request` location
   calls Doc Registry; `auth_request_set` captures the authenticated username
   from the verifier's response, and every proxying location sets
   `X-SpecGate-User` from that captured value — never from the client. This is
   the same treatment `X-SpecGate-Internal-Agent` already gets, extended to one
   more header, and the existing release test grows to cover both.
4. **Machine-to-machine endpoints skip authentication explicitly**: the health
   probes, the OAuth provider redirect, and the integration webhooks, which
   authenticate by payload signature. Each carries `auth_request off` with the
   reason beside it.
5. **The authenticated identity outranks the request body.** Where the header is
   present it *is* the actor; `approved_by` and `decided_by` are ignored rather
   than merged, and keep working only for deployments with no configured
   credentials. Without this step the rest is decoration — the caller would
   authenticate and then name someone else.
6. **The verifier is one endpoint, not an authorization framework.** It checks a
   Basic credential against the member store and answers with the username. No
   sessions, no cookies, no tokens, no roles, no middleware chain: `AGENTS.md`
   §7's ban on JWT/RBAC middleware stands, and this ADR is the architecture
   change that permits the single endpoint.
7. **The CLI stores one credential per server**, mode `0600`, never printed, sent
   only to the server it was stored for, and it records the authenticated
   username as its local actor so the two cannot drift.
8. **Encrypted transport is required beyond loopback.** For this size of team the
   recommended path is an existing private overlay network (Tailscale, WireGuard)
   rather than certificate work; TLS at the gateway stays supported. Binding
   beyond loopback with no credentials configured must warn.

What this still does not do: it proves which credential authenticated, not who
was at the keyboard. A developer who hands their credential to a teammate — or to
their coding agent — has delegated their name, and the ledger will say so without
knowing it. The documentation keeps saying this plainly.

## Consequences

- The two separation-of-duties rules stop being best-effort string comparisons
  and start comparing an authenticated identity to the completion's agent. The
  central claim — a named human approved this exact version — becomes true in a
  shared deployment rather than assumed.
- Membership has exactly one home. Adding or removing a developer is a database
  change through the existing member surface, backup and restore already cover
  it, and revocation costs no re-keying for anyone else.
- Every request pays one internal HTTP call. On a loopback appliance serving
  three developers this is irrelevant; it would not be on a public service, and
  this design is not for one.
- The verifier is a new trust-boundary primitive. If it fails open on an
  unexpected error, the appliance silently unauthenticates; if it fails closed
  while unconfigured, every existing install breaks. Both directions need tests,
  not review vigilance.
- `approved_by` / `decided_by` keep their meaning only for ungated deployments.
  Their schema comments ("no HTTP auth; supplied in the request") need updating,
  and the API contract has to state the precedence.
- Docs to update on implementation: `trust-and-security.md` (recipe, transport,
  and the delegation limit), `operate-specgate.md` (sharing an appliance, adding
  and removing developers), the configuration reference, `docs/contributing/architecture.md`
  and `app/doc-registry/AGENTS.md` §7 (the gateway is an enforcement point, the
  verifier endpoint is the permitted exception, and why there is still no
  middleware), and `docs/contributing/contracts.md` for the actor precedence.
- Tests: gateway configuration assertions (identity header set from the verifier
  in every proxying location; `auth_request off` only on the machine endpoints),
  verifier behaviour for unconfigured / valid / invalid / unknown-user, header
  outranks body in Doc Registry, CLI credential round-trip at mode `0600` with no
  secret in output and no cross-server reuse, and the
  non-loopback-without-credentials warning.

## Follow-up completed — 2026-07-30

Issuing and revoking a credential left no trail, which this ADR shipped without
and the sweep recorded as an open gap. Closed with an `identity_events` table:
every access change appends a row naming the member, the actor, and the
workspace, a change that cannot be recorded fails the request, and
`workspace members` reports each member's credential state with its most recent
change. The rows are append-only by convention and are not hash-chained, so the
documentation says the trail shows what the service recorded rather than calling
it tamper-evident.

## Not in scope

Sessions, cookies, SSO, OAuth, per-user scopes or roles, browser pairing codes
(option F), and cryptographically signed acceptance records. Each is a separate
decision, and none is required to close the case this ADR names.
