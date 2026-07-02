package integrations

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"
	"time"
)

// webhookSecretFakeStore embeds Store (nil) and overrides only the two methods
// the webhook-secret service touches, keeping the stored ciphertext in memory so
// get-or-create and rotate behavior is observable across calls.
type webhookSecretFakeStore struct {
	Store
	rows map[string]*Integration
}

func (f *webhookSecretFakeStore) GetIntegration(_ context.Context, id string) (*Integration, error) {
	if row, ok := f.rows[id]; ok {
		clone := *row
		clone.HasWebhookSecret = row.WebhookSecretEncrypted != ""
		return &clone, nil
	}
	return nil, ErrNotFound
}

func (f *webhookSecretFakeStore) UpdateWebhookSecretEncrypted(_ context.Context, id string, encrypted string) error {
	row, ok := f.rows[id]
	if !ok {
		return ErrNotFound
	}
	row.WebhookSecretEncrypted = encrypted
	return nil
}

func newWebhookSecretService(t *testing.T, provider string) (*Service, string) {
	t.Helper()
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	store := &webhookSecretFakeStore{rows: map[string]*Integration{
		"int-1": {ID: "int-1", Provider: provider, Status: StatusConnected},
	}}
	return NewService(store), "int-1"
}

func validSigningToken() string {
	return "whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func TestWebhookSecret_GitHubGeneratesOnceThenStable(t *testing.T) {
	svc, id := newWebhookSecretService(t, ProviderGitHub)

	first, err := svc.WebhookSecret(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("github WebhookSecret must generate on first access")
	}
	second, err := svc.WebhookSecret(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("a second read must return the stored secret, got %q then %q", first, second)
	}
}

func TestWebhookSecret_GitLabReturnsStoredOnlyNeverGenerates(t *testing.T) {
	svc, id := newWebhookSecretService(t, ProviderGitLab)

	got, err := svc.WebhookSecret(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("gitlab WebhookSecret must not generate a token; got %q", got)
	}

	token := validSigningToken()
	if err := svc.SetWebhookSecret(context.Background(), id, token); err != nil {
		t.Fatal(err)
	}
	got, err = svc.WebhookSecret(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("after set, WebhookSecret = %q, want the pasted signing token", got)
	}
}

func TestSetWebhookSecret_GitLabValidatesSigningTokenFormat(t *testing.T) {
	svc, id := newWebhookSecretService(t, ProviderGitLab)

	if err := svc.SetWebhookSecret(context.Background(), id, "plain-secret"); !errors.Is(err, ErrValidation) {
		t.Fatalf("a non-whsec gitlab token must be a validation error, got %v", err)
	}
	if err := svc.SetWebhookSecret(context.Background(), id, validSigningToken()); err != nil {
		t.Fatalf("a valid whsec token must be accepted, got %v", err)
	}
}

func TestSetWebhookSecret_RejectsEmptyAndNonSelfServe(t *testing.T) {
	svc, id := newWebhookSecretService(t, ProviderGitHub)
	if err := svc.SetWebhookSecret(context.Background(), id, "   "); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty secret must be a validation error, got %v", err)
	}

	linearSvc, linearID := newWebhookSecretService(t, ProviderLinear)
	if err := linearSvc.SetWebhookSecret(context.Background(), linearID, "anything"); !errors.Is(err, ErrValidation) {
		t.Fatalf("linear is not self-serve; want validation error, got %v", err)
	}
	if _, err := linearSvc.WebhookSecret(context.Background(), linearID); !errors.Is(err, ErrValidation) {
		t.Fatalf("linear WebhookSecret must be a validation error, got %v", err)
	}
}

func TestRotateWebhookSecret_GitHubRotatesGitLabRejects(t *testing.T) {
	ghSvc, ghID := newWebhookSecretService(t, ProviderGitHub)
	first, err := ghSvc.WebhookSecret(context.Background(), ghID)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := ghSvc.RotateWebhookSecret(context.Background(), ghID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == "" || rotated == first {
		t.Fatalf("rotate must mint a new secret; first=%q rotated=%q", first, rotated)
	}

	glSvc, glID := newWebhookSecretService(t, ProviderGitLab)
	if _, err := glSvc.RotateWebhookSecret(context.Background(), glID); !errors.Is(err, ErrValidation) {
		t.Fatalf("gitlab is validate-only; rotate must be a validation error, got %v", err)
	}
}

func TestWebhookTimestampWithinTolerance(t *testing.T) {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if !webhookTimestampWithinTolerance(now, webhookReplayTolerance) {
		t.Fatal("a current timestamp must be within tolerance")
	}
	if webhookTimestampWithinTolerance("1700000000", webhookReplayTolerance) {
		t.Fatal("a far-past timestamp must be outside tolerance")
	}
	if webhookTimestampWithinTolerance("not-a-number", webhookReplayTolerance) {
		t.Fatal("an unparseable timestamp must be rejected")
	}
}
