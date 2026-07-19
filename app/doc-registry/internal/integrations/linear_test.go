package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Linear-Signature is a bare hex HMAC-SHA256 of the raw body — no sha256=
// prefix. The GitHub verifier (which requires the prefix) must reject it.
func TestVerifyLinearSignature_BareHexNoPrefix(t *testing.T) {
	t.Parallel()
	secret := "lin_wh_secret"
	body := []byte(`{"type":"Issue"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if !VerifyLinearSignature(secret, body, sig) {
		t.Fatal("valid bare-hex signature must verify")
	}
	if VerifyLinearSignature(secret, body, "deadbeef") {
		t.Fatal("wrong signature must not verify")
	}
	if VerifyLinearSignature(secret, body, "") {
		t.Fatal("empty signature must not verify")
	}
	if VerifyLinearSignature("", body, sig) {
		t.Fatal("empty secret must not verify")
	}
	// A GitHub-shaped sha256=<hex> header must NOT verify under Linear's bare-hex scheme.
	if VerifyLinearSignature(secret, body, "sha256="+sig) {
		t.Fatal("prefixed signature must not verify under Linear's bare-hex scheme")
	}
}

func TestValidateLinearWebhookTimestamp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	current := fmt.Sprintf(`{"webhookTimestamp":%d}`, now.UnixMilli())
	if err := validateLinearWebhookTimestamp(current, now); err != nil {
		t.Fatalf("current timestamp rejected: %v", err)
	}
	for _, payload := range []string{
		`{}`,
		`{"webhookTimestamp":0}`,
		fmt.Sprintf(`{"webhookTimestamp":%d}`, now.Add(-2*time.Minute).UnixMilli()),
		fmt.Sprintf(`{"webhookTimestamp":%d}`, now.Add(2*time.Minute).UnixMilli()),
	} {
		if err := validateLinearWebhookTimestamp(payload, now); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("payload %s error = %v, want ErrUnauthorized", payload, err)
		}
	}
}

func TestTrackerFeedbackEventVocabulary(t *testing.T) {
	t.Parallel()
	if FeedbackEventTrackerStatusChanged != "delivery.tracker_status_changed" {
		t.Fatalf("tracker feedback event = %q, want delivery.tracker_status_changed", FeedbackEventTrackerStatusChanged)
	}
}

func TestHandleLinearResourceWebhookRequiresResourceSecret(t *testing.T) {
	t.Parallel()

	const (
		payload = `{"type":"Issue"}`
	)
	store := enqueueFakeStore{
		integration: &Integration{
			ID:          "int-1",
			WorkspaceID: "ws-test",
			Provider:    ProviderLinear,
			Status:      StatusConnected,
		},
		resource: &Resource{
			ID:            "res-1",
			IntegrationID: "int-1",
			ResourceType:  ResourceTypeTeam,
			ExternalID:    "team-1",
			ExternalKey:   "ENG",
			DisplayName:   "Engineering",
			ConfigJSON:    `{}`,
		},
	}
	svc := NewService(store)
	mac := hmac.New(sha256.New, []byte("resource-secret"))
	_, _ = mac.Write([]byte(payload))

	_, err := svc.HandleLinearResourceWebhook(context.Background(), "int-1", "res-1", LinearWebhookInput{
		Signature:   hex.EncodeToString(mac.Sum(nil)),
		PayloadJSON: payload,
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized without a resource secret", err)
	}
}

func TestHandleLinearResourceWebhookRejectsDisabledIntegration(t *testing.T) {
	store := enqueueFakeStore{
		integration: &Integration{ID: "int-1", WorkspaceID: "ws-test", Provider: ProviderLinear, Status: StatusDisabled},
		resource:    &Resource{ID: "res-1", IntegrationID: "int-1", ResourceType: ResourceTypeTeam},
	}

	_, err := NewService(store).HandleLinearResourceWebhook(context.Background(), "int-1", "res-1", LinearWebhookInput{PayloadJSON: `{}`})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation for disabled integration", err)
	}
}
