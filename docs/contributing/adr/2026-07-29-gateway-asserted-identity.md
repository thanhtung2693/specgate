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

## Constraints

- Doc Registry must not grow JWT/RBAC middleware (module rule); the enforcement
  point has to be the gateway.
- Local mode gets nothing: one binary, one SQLite file, one machine. Ted's
  instruction — solo first, and Local needs no authentication.
- No password prompts, no OAuth, no identity provider. This is a small trusted
  team on a private network.
- Credentials never land in CLI configuration in plaintext-by-accident: the
  existing rule is mode `0600` and never printing secret values.

## Options

**A. Do nothing; keep documenting the trusted-network assumption.**
Honest and free. Leaves the shared-appliance case with no story, which is the
case the team pitch depends on.

**B. Shared appliance token, enforced by nginx; identity header passed through
(chosen).** One secret for the appliance. nginx requires it on `/api/*`, strips
any client-supplied identity header, and forwards the caller's declared user
name only on an authenticated request. The ledger name becomes an assertion made
by *someone holding the appliance token* rather than by anyone who can reach the
port.

**C. HTTP Basic auth at nginx.** Functionally the same shared secret, but the
browser gets a native prompt for free, so the UI is covered by the same
boundary. Rejected on instruction — no password ceremony — but recorded because
it is the smaller design: option B leaves the browser path ungated (see
Consequences), and Basic auth does not.

**D. Per-user credentials, sessions, an identity provider.**
The only option that actually proves who was at the keyboard. Out of scope for a
trusted small team, and it would put an authentication layer inside the product.

## Decision

Adopt **B**, scoped as narrowly as the problem:

1. **The token is optional and off by default.** With no
   `SPECGATE_APPLIANCE_TOKEN` set, behaviour is exactly today's — the loopback
   default stays frictionless, and nothing changes for solo users.
2. **nginx enforces it when set**: requests to the API surface without the
   matching token are refused at the gateway, before Doc Registry sees them.
   Doc Registry gains no middleware.
3. **nginx owns the identity header.** It strips any client-supplied value —
   the same treatment `X-SpecGate-Internal-Agent` already gets — and sets the
   forwarded identity from the authenticated request. A client can never assert
   an identity header directly.
4. **The CLI stores the token per server**, mode `0600`, never printed; status
   output reports set/not-set only. A missing or wrong token maps to the
   existing exit code for an unavailable/refused service, with a message naming
   the fix.
5. **Binding beyond loopback without a token warrants a warning**, so the risky
   configuration announces itself instead of being discovered later.

Identity remains self-declared *within* the trusted circle. That is the
deliberate boundary of this ADR: the token answers "may this caller write to the
ledger", and the existing string comparisons answer "is this the same identity
that filed the completion". Neither answers "who was at the keyboard", and the
documentation must keep saying so.

## Consequences

- The skeptic's disqualifying case — an audit ledger reachable and writable by
  anyone on the network — stops being reachable in a shared deployment, without
  adding an authentication layer to the product.
- The two separation-of-duties rules become meaningful in that deployment: an
  outsider can no longer file a completion under one name and approve it under
  another, because they cannot write at all.
- **The browser path stays ungated.** nginx serves the UI to whoever reaches it;
  a browser cannot hold a bearer token without a login step, which this ADR
  excludes by instruction. If the appliance is bound beyond loopback, the UI is
  exposed even when the API is gated. This is the price of option B over option
  C and it must be stated in the user documentation, not discovered.
- One shared secret means no per-user revocation: rotating it re-keys every
  client.
- Docs to update when this is implemented: `trust-and-security.md` (gains the
  recipe and the browser limit), `operate-specgate.md` (sharing an appliance),
  the configuration reference and `.env.example` (the new variable), and
  `docs/contributing/architecture.md` plus `app/doc-registry/AGENTS.md` §7 (the
  gateway is now an enforcement point, and why Doc Registry still has no
  middleware).
- Tests this needs: gateway config assertions beside the existing
  internal-header test (identity header stripped, API refused without the token,
  allowed with it), CLI config round-trip at mode `0600` with no secret in
  output, and the non-loopback-without-token warning.

## Not in scope

Per-user authentication, sessions, SSO, per-user tokens or revocation, browser
login, and signed acceptance records. Each is a separate decision, and none is
needed to close the case this ADR names.
