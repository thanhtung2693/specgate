package api

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/specgate/doc-registry/internal/identity"
	"github.com/specgate/doc-registry/internal/workspace"
)

func credentialHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}

func basicHeader(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// An appliance with no member credentials must keep answering. Every existing
// loopback install is in this state, and refusing here would lock all of them out
// the moment the gateway starts calling the verifier.
func TestVerifyGatewayCredentialAllowsUnconfiguredAppliance(t *testing.T) {
	t.Parallel()
	h := &Handlers{Identity: &fakeIdentityStore{}}

	out, err := h.VerifyGatewayCredential(context.Background(), &GatewayAuthInput{})
	if err != nil {
		t.Fatalf("unconfigured appliance must stay open, got %v", err)
	}
	if out.User != "" {
		t.Fatalf("identity = %q, want empty when nobody can authenticate", out.User)
	}
}

func TestVerifyGatewayCredentialAcceptsAMember(t *testing.T) {
	t.Parallel()
	h := &Handlers{Identity: &fakeIdentityStore{
		credentials: map[string]string{"tung": credentialHash(t, "s3cret")},
	}}

	out, err := h.VerifyGatewayCredential(context.Background(), &GatewayAuthInput{
		Authorization: basicHeader("tung", "s3cret"),
	})
	if err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
	if out.User != "tung" {
		t.Fatalf("identity = %q, want tung; the gateway forwards this value as the actor", out.User)
	}
}

// Every failing shape must look the same to the caller: a wrong password, an
// unknown user, a member with no credential, and a missing or malformed header.
// A distinguishable answer would turn the verifier into a username oracle.
func TestVerifyGatewayCredentialRefusesEveryOtherShape(t *testing.T) {
	t.Parallel()
	store := &fakeIdentityStore{credentials: map[string]string{
		"tung":     credentialHash(t, "s3cret"),
		"nocreds":  "",
		"upperref": credentialHash(t, "s3cret"),
	}}
	h := &Handlers{Identity: store}

	for _, tc := range []struct {
		name   string
		header string
	}{
		{"wrong password", basicHeader("tung", "wrong")},
		{"unknown user", basicHeader("ghost", "s3cret")},
		{"member without a credential", basicHeader("nocreds", "s3cret")},
		{"no header at all", ""},
		{"not basic", "Bearer token"},
		{"undecodable", "Basic ????"},
		{"no colon", "Basic " + base64.StdEncoding.EncodeToString([]byte("tung"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := h.VerifyGatewayCredential(context.Background(), &GatewayAuthInput{Authorization: tc.header}); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}
}

// An unreadable member store must not silently unauthenticate an appliance that
// was configured to require credentials. Fail closed.
func TestVerifyGatewayCredentialFailsClosedOnStoreError(t *testing.T) {
	t.Parallel()
	h := &Handlers{Identity: &fakeIdentityStore{storeErr: errors.New("database is down")}}

	if _, err := h.VerifyGatewayCredential(context.Background(), &GatewayAuthInput{
		Authorization: basicHeader("tung", "s3cret"),
	}); err == nil {
		t.Fatal("a broken credential store must refuse, not open the gateway")
	}
}

// The whole point of authenticating a caller is that the ledger records who they
// are. If a body-supplied actor could still win, someone would authenticate as
// themselves and approve under a colleague's name.
func TestResolveActorPrefersTheAuthenticatedIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		authenticated string
		declared      string
		want          string
	}{
		{"authenticated identity wins over a different body actor", "tung", "boss", "tung"},
		{"authenticated identity wins over an empty body actor", "tung", "", "tung"},
		{"body actor is used when no gateway authenticated the caller", "", "tung", "tung"},
		{"whitespace-only header does not mask the body actor", "   ", "tung", "tung"},
		{"both empty stays empty for the caller to reject", "", "", ""},
		{"values are trimmed", " tung ", "boss", "tung"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveActor(tc.authenticated, tc.declared); got != tc.want {
				t.Fatalf("resolveActor(%q, %q) = %q, want %q", tc.authenticated, tc.declared, got, tc.want)
			}
		})
	}
}

// Every write that records who decided something must read the header. A new
// endpoint that records an actor and forgets it would silently accept a
// self-declared name again, and no other test would notice.
func TestEveryActorRecordingInputCarriesTheAuthenticatedHeader(t *testing.T) {
	t.Parallel()
	for _, in := range []any{
		&CLIDeliveryDecisionInput{},
		&CLIArchiveWorkItemInput{},
		&UpdateStatusInput{},
		&PromoteArtifactCanonicalInput{},
		&UnarchiveChangeRequestInput{},
		// Issuing a credential records an actor on its access-change row, so it is
		// bound by the same rule as the decision endpoints.
		&IssueCredentialInput{},
	} {
		value := reflect.ValueOf(in).Elem()
		if _, ok := value.Type().FieldByName("AuthenticatedActorHeader"); !ok {
			t.Fatalf("%s records an actor without embedding AuthenticatedActorHeader", value.Type().Name())
		}
	}
}

// Issuing returns the secret once and stores only a hash, so the value the
// operator copies is the only copy that exists in plaintext.
func TestIssueGatewayCredentialReturnsTheSecretOnceAndStoresAHash(t *testing.T) {
	t.Parallel()
	store := &fakeIdentityStore{knownUsers: map[string]bool{"tung": true}}
	h := &Handlers{Identity: store}

	out, err := h.IssueGatewayCredential(context.Background(), &IssueCredentialInput{Username: "tung"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Secret == "" || !out.Body.CredentialSet {
		t.Fatalf("output = %#v, want a generated secret", out.Body)
	}
	stored := store.credentials["tung"]
	if stored == out.Body.Secret {
		t.Fatal("the credential was stored in plaintext")
	}
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte(out.Body.Secret)) != nil {
		t.Fatal("the stored hash does not verify the issued secret")
	}
	// The issued secret must actually authenticate afterwards.
	if _, err := h.VerifyGatewayCredential(context.Background(), &GatewayAuthInput{
		Authorization: basicHeader("tung", out.Body.Secret),
	}); err != nil {
		t.Fatalf("issued credential does not authenticate: %v", err)
	}
}

// Two issues for the same member must not produce the same secret, or rotation
// would not actually rotate.
func TestIssueGatewayCredentialRotates(t *testing.T) {
	t.Parallel()
	store := &fakeIdentityStore{knownUsers: map[string]bool{"tung": true}}
	h := &Handlers{Identity: store}

	first, err := h.IssueGatewayCredential(context.Background(), &IssueCredentialInput{Username: "tung"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.IssueGatewayCredential(context.Background(), &IssueCredentialInput{Username: "tung"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Body.Secret == second.Body.Secret {
		t.Fatal("rotation reissued the same secret")
	}
	if _, err := h.VerifyGatewayCredential(context.Background(), &GatewayAuthInput{
		Authorization: basicHeader("tung", first.Body.Secret),
	}); err == nil {
		t.Fatal("the previous secret still authenticates after rotation")
	}
}

func TestIssueGatewayCredentialRevokeAndUnknownMember(t *testing.T) {
	t.Parallel()
	store := &fakeIdentityStore{knownUsers: map[string]bool{"tung": true}}
	h := &Handlers{Identity: store}
	issued, err := h.IssueGatewayCredential(context.Background(), &IssueCredentialInput{Username: "tung"})
	if err != nil {
		t.Fatal(err)
	}

	revoke := &IssueCredentialInput{Username: "tung"}
	revoke.Body.Revoke = true
	out, err := h.IssueGatewayCredential(context.Background(), revoke)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Secret != "" || out.Body.CredentialSet {
		t.Fatalf("revoke returned %#v, want no secret", out.Body)
	}
	if _, ok := store.credentials["tung"]; ok {
		t.Fatal("revoke left the credential in place")
	}
	// With the last credential gone the appliance is unconfigured again, so the
	// verifier opens rather than locking everyone out.
	if _, err := h.VerifyGatewayCredential(context.Background(), &GatewayAuthInput{
		Authorization: basicHeader("tung", issued.Body.Secret),
	}); err != nil {
		t.Fatalf("an appliance with no credentials left must stay open, got %v", err)
	}

	// Membership is granted elsewhere: issuing must not invent a member.
	if _, err := h.IssueGatewayCredential(context.Background(), &IssueCredentialInput{Username: "ghost"}); err == nil {
		t.Fatal("issued a credential for a member this appliance does not have")
	}
}

// Granting and revoking access are governance acts. Before this, both changed who
// could authenticate and left nothing behind — on a product whose claim is an
// acceptance ledger.
func TestIssueGatewayCredentialRecordsTheAccessChange(t *testing.T) {
	t.Parallel()
	store := &fakeIdentityStore{knownUsers: map[string]bool{"mai": true}}
	h := &Handlers{Identity: store}

	// The workspace arrives in the request scope, the way the CLI sends it; a body
	// value that disagrees with the scope is refused by applyCLIWorkspace.
	ctx := workspace.WithID(context.Background(), "workspace-1")
	issue := &IssueCredentialInput{Username: "mai"}
	issue.AuthenticatedUser = "tung"
	if _, err := h.IssueGatewayCredential(ctx, issue); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %d, want one issue record", len(store.events))
	}
	got := store.events[0]
	if got.EventType != identity.EventCredentialIssued || got.Username != "mai" || got.Actor != "tung" {
		t.Fatalf("event = %#v, want an issue record for mai by tung", got)
	}
	if got.WorkspaceID != "workspace-1" {
		t.Fatalf("event workspace = %q, want the workspace the operator acted in", got.WorkspaceID)
	}

	revoke := &IssueCredentialInput{Username: "mai"}
	revoke.AuthenticatedUser = "tung"
	revoke.Body.Revoke = true
	if _, err := h.IssueGatewayCredential(ctx, revoke); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 2 || store.events[1].EventType != identity.EventCredentialRevoked {
		t.Fatalf("events = %#v, want the revoke appended", store.events)
	}
}

// An unauthenticated caller can issue the first credential, since that is how a
// gated appliance gets started. The record has to say so rather than attributing
// the change to nobody in particular.
func TestIssueGatewayCredentialRecordsAnUnauthenticatedIssuer(t *testing.T) {
	t.Parallel()
	store := &fakeIdentityStore{knownUsers: map[string]bool{"mai": true}}
	h := &Handlers{Identity: store}

	if _, err := h.IssueGatewayCredential(context.Background(), &IssueCredentialInput{Username: "mai"}); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %d, want one", len(store.events))
	}
	if store.events[0].Actor != "" || store.events[0].Detail == "" {
		t.Fatalf("event = %#v, want an empty actor and a detail naming the situation", store.events[0])
	}
}

// A credential change that cannot be recorded must fail. Reporting success would
// hand out access with no trace of it, which is worse than refusing.
func TestIssueGatewayCredentialFailsWhenTheChangeCannotBeRecorded(t *testing.T) {
	t.Parallel()
	store := &fakeIdentityStore{
		knownUsers: map[string]bool{"mai": true},
		eventErr:   errors.New("identity_events unavailable"),
	}
	h := &Handlers{Identity: store}

	if _, err := h.IssueGatewayCredential(context.Background(), &IssueCredentialInput{Username: "mai"}); err == nil {
		t.Fatal("issuing succeeded while its access-change record failed")
	}
}
