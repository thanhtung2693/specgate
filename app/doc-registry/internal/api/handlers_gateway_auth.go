package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/identity"
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

	huma.Register(api, huma.Operation{
		OperationID: "gateway_credential_issue",
		Method:      http.MethodPut,
		Path:        "/identity/users/{username}/credential",
		Summary: "Issue or rotate one member's gateway credential, returning the generated secret exactly once, or " +
			"revoke it. Only a bcrypt hash is stored, so a lost secret is reissued rather than recovered",
		Tags: []string{"identity"},
	}, h.IssueGatewayCredential)
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

// timingReferenceHash is compared against when the username is unknown, so an
// unknown user costs the same bcrypt round as a wrong password and the response
// time does not reveal which it was. It is generated at startup from throwaway
// randomness: a literal hash in the source reads like a leaked credential to a
// scanner, and nothing needs this value to be stable.
var timingReferenceHash = func() []byte {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		// Fall back to a fixed cost rather than failing startup; the only property
		// that matters here is that a comparison happens at all.
		seed = []byte("specgate-timing-reference")
	}
	hash, err := bcrypt.GenerateFromPassword(seed, bcrypt.DefaultCost)
	if err != nil {
		return []byte{}
	}
	return hash
}()

// gatewayAuthChallenge keeps every refusal identical: a wrong password, an
// unknown user, and a malformed header must not be distinguishable.
func gatewayAuthChallenge() error {
	return huma.Error401Unauthorized("gateway credential required")
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

// Issuing credentials (ADR decision 5: the appliance manages its own members, so
// nobody needs external htpasswd tooling and nobody invents a password).
//
// The secret is generated here and returned exactly once. It is stored only as a
// bcrypt hash, so a lost secret is reissued rather than recovered.
//
// Access: any caller the gateway lets through may issue or rotate a credential.
// With no credentials configured that is anyone who can reach the appliance,
// which is how the first credential gets created; afterwards it is the
// authenticated members. Roles are out of scope for a three-developer appliance
// and the documentation says so.
type IssueCredentialInput struct {
	AuthenticatedActorHeader
	Username string `path:"username" doc:"Member whose gateway credential is being issued or rotated."`
	Body     struct {
		Revoke      bool   `json:"revoke,omitempty" doc:"Remove this member's credential instead of issuing one."`
		WorkspaceID string `json:"workspace_id,omitempty" doc:"Workspace the operator is acting in; recorded on the access-change event."`
	}
}

type IssueCredentialOutput struct {
	Body struct {
		Username string `json:"username"`
		// Secret is returned once, at issue time, and never stored in plaintext.
		Secret        string `json:"secret,omitempty"`
		CredentialSet bool   `json:"credential_set"`
	}
}

func (h *Handlers) IssueGatewayCredential(ctx context.Context, in *IssueCredentialInput) (*IssueCredentialOutput, error) {
	if err := h.requireService(h.Identity, "identity"); err != nil {
		return nil, err
	}
	username, err := identity.NormalizeUsername(in.Username)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	out := &IssueCredentialOutput{}
	out.Body.Username = username

	if in.Body.Revoke {
		if err := h.Identity.SetUserCredential(ctx, username, ""); err != nil {
			return nil, mapIdentityCredentialError(err)
		}
		if err := h.recordCredentialChange(ctx, in, username, identity.EventCredentialRevoked); err != nil {
			return nil, err
		}
		return out, nil
	}

	secret, err := generateCredentialSecret()
	if err != nil {
		return nil, huma.Error500InternalServerError("could not generate a credential")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, huma.Error500InternalServerError("could not hash the credential")
	}
	if err := h.Identity.SetUserCredential(ctx, username, string(hash)); err != nil {
		return nil, mapIdentityCredentialError(err)
	}
	if err := h.recordCredentialChange(ctx, in, username, identity.EventCredentialIssued); err != nil {
		return nil, err
	}
	out.Body.Secret = secret
	out.Body.CredentialSet = true
	return out, nil
}

// recordCredentialChange appends the access-change record. A failure here fails
// the request: a credential change nobody can see afterwards is the gap this
// record exists to close, and reporting success would hide it.
func (h *Handlers) recordCredentialChange(
	ctx context.Context, in *IssueCredentialInput, username, eventType string,
) error {
	actor := strings.TrimSpace(in.AuthenticatedUser)
	detail := "issued by an unauthenticated caller on an appliance with no gateway credentials"
	if actor != "" {
		detail = ""
	}
	err := h.Identity.RecordCredentialEvent(ctx, identity.CredentialEventInput{
		Username:    username,
		EventType:   eventType,
		Actor:       actor,
		Detail:      detail,
		WorkspaceID: strings.TrimSpace(in.Body.WorkspaceID),
	})
	if err != nil {
		return huma.Error503ServiceUnavailable("the access change could not be recorded")
	}
	return nil
}

func mapIdentityCredentialError(err error) error {
	if errors.Is(err, identity.ErrUserNotFound) {
		return huma.Error404NotFound("no such member on this appliance")
	}
	return huma.Error503ServiceUnavailable("gateway credential store is unavailable")
}

// generateCredentialSecret returns 32 bytes of randomness, base64url encoded so
// it survives shell copy-paste and an Authorization header unchanged.
func generateCredentialSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
