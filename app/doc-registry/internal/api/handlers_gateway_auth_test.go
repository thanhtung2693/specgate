package api

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/crypto/bcrypt"
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
	} {
		value := reflect.ValueOf(in).Elem()
		if _, ok := value.Type().FieldByName("AuthenticatedActorHeader"); !ok {
			t.Fatalf("%s records an actor without embedding AuthenticatedActorHeader", value.Type().Name())
		}
	}
}
