package integrations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
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

func TestTrackerFeedbackEventVocabulary(t *testing.T) {
	t.Parallel()
	if FeedbackEventTrackerStatusChanged != "delivery.tracker_status_changed" {
		t.Fatalf("tracker feedback event = %q, want delivery.tracker_status_changed", FeedbackEventTrackerStatusChanged)
	}
}
