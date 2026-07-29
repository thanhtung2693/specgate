# ADR: Gateway-asserted identity for the shared appliance

## Status

Proposed — 2026-07-29. Supersedes nothing. Constrains
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
among the people who all hold the secret — so it does not answer the question
this work exists to answer. It also cannot gate the browser, because a browser
holds no bearer token without a login page to put it there.

**C. Per-user credentials at the gateway, identity taken from the authenticated
user (chosen).** One line per developer in a bcrypt credential file. nginx
authenticates, then *overwrites* the identity header from the authenticated user,
so the name in the ledger is the one that authenticated. The browser's own
credential manager stores the credential after one prompt, which means the
browser is covered by the same mechanism with no session store, no cookie, no
expiry, and no login page to build.

**D. Browser pairing code (Ted's idea).** The CLI prints a code, the developer
types it into the UI, and the browser gets a session. A real pattern, and better
UX than a credential prompt. It does not replace option C, it layers on top:
the code proves the person also controls an already-authenticated CLI, so the CLI
still needs a credential first. It costs a session store, a code
issue/verify/expiry path, and cookie handling — an authentication layer inside
the product, which the module rule pushes away from Doc Registry. Since the
browser already remembers the credential in option C, this buys convenience, not
capability. Recorded as the upgrade to reach for if the prompt proves annoying.

**E. mTLS or an identity provider.** Stronger, and far more ceremony than three
developers on a private network will accept.

## Decision

Adopt **C**, and keep it as small as it can be:

1. **Off by default.** With no credential file configured, behaviour is exactly
   today's: loopback, no prompt, nothing to configure. Solo and Local are
   untouched.
2. **nginx authenticates and owns identity.** When the credential file is
   configured, the gateway requires it for the UI and the API surface, then sets
   the identity header from the authenticated user, overwriting whatever the
   client sent. This is the treatment `X-SpecGate-Internal-Agent` already gets
   and the existing release test asserts, extended to one more header. Doc
   Registry gains no middleware.
3. **The authenticated identity outranks the request body.** Where the header is
   present, it *is* the actor; `approved_by` and `decided_by` are ignored rather
   than merged, and the body fields keep working only for deployments with no
   gateway credential. Without this step the rest is decoration — the caller
   would authenticate and then name somebody else.
4. **The CLI stores one credential per server**, mode `0600`, never printed, and
   sends it on every call to that server. It records the authenticated username
   as its local actor so the two cannot drift apart.
5. **The appliance manages members itself.** A CLI command adds and removes
   developers and prints a generated credential once, so nobody needs external
   htpasswd tooling and nobody invents a password. Removing a line revokes one
   developer without re-keying the others.
6. **Encrypted transport is required beyond loopback**, and the recommended path
   for this size of team is an existing private overlay network (Tailscale,
   WireGuard) rather than certificate work: it encrypts the hop and removes the
   public exposure question entirely. TLS at the gateway stays supported for
   teams that prefer it. Binding beyond loopback with no credential file
   configured must warn.

What this still does not do: it proves which credential authenticated, not who
was at the keyboard. A developer who hands their credential to someone else — or
to their coding agent — has delegated their name, and the ledger will say so
without knowing it. The documentation keeps saying this plainly.

## Consequences

- The two separation-of-duties rules stop being best-effort string comparisons
  and start comparing an authenticated identity to the completion's agent. The
  product's central claim — a named human approved this exact version — becomes
  true in a shared deployment rather than assumed.
- The disqualifying case for a shared appliance (anyone who reaches the port can
  write governance decisions) closes, and the browser closes with it, which
  option B could not do.
- Per-developer revocation costs one line. There is no shared secret to rotate.
- Basic credentials are reversible on the wire, so the transport requirement in
  decision 6 is not advisory. A private overlay network satisfies it with less
  work than certificates.
- The identity header becomes a trust-boundary primitive: every gateway config
  must set it from the authenticated user, and any new gateway or route that
  forgets is a spoofing hole. This belongs in the release suite next to the
  existing internal-header assertion, not in a review checklist.
- `approved_by` / `decided_by` keep their meaning only for ungated deployments.
  Their schema comments ("no HTTP auth; supplied in the request") need updating,
  and the API contract has to state the precedence.
- Docs to update on implementation: `trust-and-security.md` (recipe, transport,
  and the delegation limit), `operate-specgate.md` (sharing an appliance, adding
  and removing developers), the configuration reference and `.env.example`,
  `docs/contributing/architecture.md` and `app/doc-registry/AGENTS.md` §7 (the
  gateway is now an enforcement point and why Doc Registry still has no
  middleware), and `docs/contributing/contracts.md` for the actor precedence.
- Tests: gateway config assertions (identity header always set from the
  authenticated user; API and UI refused without credentials), header-outranks-
  body precedence in Doc Registry, CLI credential round-trip at mode `0600` with
  no secret in output, member add/remove preserving the other members, and the
  non-loopback-without-credentials warning.

## Not in scope

Sessions, cookies, SSO, OAuth, per-user token scopes, browser pairing codes
(option D), and cryptographically signed acceptance records. Each is a separate
decision, and none is required to close the case this ADR names.
