package api

import "strings"

// Actor precedence for recorded decisions (ADR 2026-07-29 gateway-asserted
// identity).
//
// The actor on an approval or a delivery decision used to come from the request
// body, because there was no HTTP authentication to take it from. Where the
// gateway authenticated the caller it forwards the verified username in
// `X-SpecGate-User`, and that value wins: authenticating a caller and then
// recording whatever name their body carried would leave the ledger exactly as
// self-declared as before.
//
// The body value keeps working for a deployment with no gateway credentials,
// which is every default appliance. Nothing here decides *whether* a caller may
// act — the gateway already refused unauthenticated callers — so this is a
// precedence rule, not an authorization layer.
//
// The gateway blanks the header on every route, so a non-empty value here can
// only have come from the verifier.
func resolveActor(authenticated, declared string) string {
	if actor := strings.TrimSpace(authenticated); actor != "" {
		return actor
	}
	return strings.TrimSpace(declared)
}

// AuthenticatedActorHeader is the header nginx sets from the verifier's answer.
// Embedded in the write inputs that record who decided something.
type AuthenticatedActorHeader struct {
	AuthenticatedUser string `header:"X-SpecGate-User" doc:"Set by the gateway from the authenticated caller; overrides any actor supplied in the body. Clients cannot set it — the gateway blanks it on every route."`
}
