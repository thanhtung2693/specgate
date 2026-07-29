package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/crypto/bcrypt"
)

// Gateway credential verification (ADR 2026-07-29 gateway-asserted identity).
//
// The gateway calls this endpoint through nginx `auth_request` for every request
// it serves, then takes the caller's identity from the response header. That is
// the whole authentication surface: one credential check against the member
// store. There are no sessions, tokens, cookies, or roles here, so the module
// rule against JWT/RBAC middleware still holds.
//
// Two invariants matter more than the code:
//
//   - With no credential configured for anybody, this answers 200 with an empty
//     identity. A default loopback appliance keeps working untouched, which is
//     the posture a solo install expects.
//   - The identity travels only in this response. nginx overwrites the forwarded
//     header from it, so a client can never assert who it is.
type GatewayAuthInput struct {
	Authorization string `header:"Authorization" doc:"Basic credential presented by the caller."`
}

type GatewayAuthOutput struct {
	// User is read by nginx via auth_request_set and forwarded as the caller's
	// identity. It is empty when authentication is not configured.
	User        string `header:"X-SpecGate-User"`
	Realm       string `header:"WWW-Authenticate"`
	CacheStatus string `header:"Cache-Control"`
}

func (h *Handlers) registerGatewayAuth(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "gateway_auth_verify",
		Method:      http.MethodGet,
		Path:        "/internal/auth",
		Summary: "Verify a gateway credential and return the authenticated username. Called by the gateway's " +
			"auth_request, never by a client: the gateway marks its location internal. Answers 200 with an empty " +
			"identity when no member has a credential, so an unconfigured appliance stays open",
		Tags: []string{"system"},
	}, h.VerifyGatewayCredential)
}

// VerifyGatewayCredential authenticates one Basic credential against the member
// store.
func (h *Handlers) VerifyGatewayCredential(ctx context.Context, in *GatewayAuthInput) (*GatewayAuthOutput, error) {
	if err := h.requireService(h.Identity, "identity"); err != nil {
		return nil, err
	}
	configured, err := h.Identity.CredentialsConfigured(ctx)
	if err != nil {
		// Fail closed: an unreadable member store must not silently unauthenticate
		// an appliance that was configured to require credentials.
		return nil, huma.Error503ServiceUnavailable("gateway credential store is unavailable")
	}
	if !configured {
		return &GatewayAuthOutput{CacheStatus: "no-store"}, nil
	}

	username, password, ok := basicCredential(in.Authorization)
	if !ok {
		return nil, gatewayAuthChallenge()
	}
	stored, err := h.Identity.UserCredential(ctx, username)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("gateway credential store is unavailable")
	}
	if stored == "" {
		// Unknown user or no credential. Compare against a fixed hash anyway so an
		// unknown username costs the same time as a wrong password.
		_ = bcrypt.CompareHashAndPassword(timingReferenceHash, []byte(password))
		return nil, gatewayAuthChallenge()
	}
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) != nil {
		return nil, gatewayAuthChallenge()
	}
	return &GatewayAuthOutput{User: username, CacheStatus: "no-store"}, nil
}

// timingReferenceHash is a bcrypt hash of a value nobody uses, compared against
// when the username is unknown so the response time does not reveal whether the
// user exists.
var timingReferenceHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

func gatewayAuthChallenge() error {
	err := huma.Error401Unauthorized("gateway credential required")
	return err
}

// basicCredential decodes an HTTP Basic Authorization header.
func basicCredential(header string) (username, password string, ok bool) {
	const prefix = "Basic "
	value := strings.TrimSpace(header)
	if len(value) <= len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found || username == "" {
		return "", "", false
	}
	return username, password, true
}
