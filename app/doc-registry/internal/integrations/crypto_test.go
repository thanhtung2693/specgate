package integrations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// gitLabSig computes the Standard Webhooks v1 signature GitLab sends, so the
// test verifies against the same algorithm the production code uses.
func gitLabSig(t *testing.T, signingToken, id, ts string, body []byte) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(signingToken, "whsec_"))
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestIsGitLabSigningToken(t *testing.T) {
	good := "whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 32))
	if !IsGitLabSigningToken(good) {
		t.Fatal("a whsec_ + base64(32-byte) token must be accepted")
	}
	if IsGitLabSigningToken("whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 16))) {
		t.Fatal("a key shorter than 32 bytes must be rejected")
	}
	if IsGitLabSigningToken(base64.StdEncoding.EncodeToString(make([]byte, 32))) {
		t.Fatal("a token without the whsec_ prefix must be rejected")
	}
	if IsGitLabSigningToken("whsec_not-base64!!") {
		t.Fatal("a non-base64 body must be rejected")
	}
}

func TestVerifyGitLabSigningToken(t *testing.T) {
	token := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	id, ts := "msg_123", "1700000000"
	body := []byte(`{"object_kind":"merge_request"}`)
	valid := gitLabSig(t, token, id, ts, body)

	if !VerifyGitLabSigningToken(token, id, ts, body, valid) {
		t.Fatal("a correct signature must verify")
	}
	// Standard Webhooks: webhook-signature is a space-separated list; match any.
	if !VerifyGitLabSigningToken(token, id, ts, body, "v1,deadbeef "+valid) {
		t.Fatal("a matching signature among several must verify")
	}
	other := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	if VerifyGitLabSigningToken(token, id, ts, body, gitLabSig(t, other, id, ts, body)) {
		t.Fatal("a signature made with a different token must be rejected")
	}
	if VerifyGitLabSigningToken(token, id, "1700000001", body, valid) {
		t.Fatal("a changed timestamp must break the signature")
	}
	if VerifyGitLabSigningToken(token, id, ts, []byte("tampered"), valid) {
		t.Fatal("a tampered body must be rejected")
	}
	if VerifyGitLabSigningToken("", id, ts, body, valid) || VerifyGitLabSigningToken(token, "", ts, body, valid) ||
		VerifyGitLabSigningToken(token, id, "", body, valid) || VerifyGitLabSigningToken(token, id, ts, body, "") {
		t.Fatal("a blank token/id/timestamp/signature must be rejected")
	}
	if VerifyGitLabSigningToken("not-a-whsec-token", id, ts, body, valid) {
		t.Fatal("a malformed token must be rejected")
	}
}

func TestVerifyGitHubSignature(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"action":"opened","number":7}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	valid := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifyGitHubSignature(secret, body, valid) {
		t.Fatal("a correct signature must verify")
	}
	if VerifyGitHubSignature(secret, body, "sha256=deadbeef") {
		t.Fatal("a wrong signature must be rejected")
	}
	if VerifyGitHubSignature(secret, body, "") {
		t.Fatal("an empty signature must be rejected")
	}
	if VerifyGitHubSignature("", body, valid) {
		t.Fatal("an empty secret must be rejected (misconfigured integration)")
	}
	if VerifyGitHubSignature(secret, body, hex.EncodeToString(mac.Sum(nil))) {
		t.Fatal("a signature missing the sha256= prefix must be rejected")
	}
	if VerifyGitHubSignature(secret, []byte("tampered"), valid) {
		t.Fatal("a body that does not match the signature must be rejected")
	}
}

func TestHashWebhookSecret_StableAndCaseSensitive(t *testing.T) {
	a := HashWebhookSecret("hunter2")
	b := HashWebhookSecret("hunter2")
	if a != b || a == "" {
		t.Fatalf("hash should be stable and non-empty, got a=%q b=%q", a, b)
	}
	if HashWebhookSecret("Hunter2") == a {
		t.Fatal("hash should be case-sensitive")
	}
	if len(a) != 64 {
		t.Fatalf("expected sha256 hex length 64, got %d", len(a))
	}
}

func TestHashWebhookSecret_TrimsWhitespaceSoCopyPasteSurvivesNewlines(t *testing.T) {
	canonical := HashWebhookSecret("hunter2")
	if HashWebhookSecret("  hunter2\n") != canonical {
		t.Fatal("expected leading/trailing whitespace to be trimmed before hashing")
	}
}

func TestVerifyWebhookSecret_RejectsBlanksAndMismatches(t *testing.T) {
	stored := HashWebhookSecret("real-secret")
	if !VerifyWebhookSecret(stored, "real-secret") {
		t.Fatal("matching secret should verify")
	}
	if VerifyWebhookSecret(stored, "wrong-secret") {
		t.Fatal("mismatched secret must not verify")
	}
	if VerifyWebhookSecret("", "real-secret") {
		t.Fatal("empty stored hash must reject — otherwise unconfigured integrations become open relays")
	}
	if VerifyWebhookSecret(stored, "") {
		t.Fatal("empty inbound token must reject — defends against accidental blank header")
	}
}

func TestEncryptDecryptSecret_RoundTripsAndIsRandomized(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, strings.Repeat("a1b2c3d4", 8)) // 64 hex chars = 32 bytes

	first, err := EncryptSecret("glpat-redacted-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	second, err := EncryptSecret("glpat-redacted-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if first == second {
		t.Fatal("AES-GCM with random nonce must produce distinct ciphertexts for the same plaintext")
	}
	for _, ct := range []string{first, second} {
		if !strings.HasPrefix(ct, "v1:") {
			t.Fatalf("ciphertext must carry version prefix: %q", ct)
		}
		got, err := DecryptSecret(ct)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if got != "glpat-redacted-token" {
			t.Fatalf("decrypted plaintext mismatch: %q", got)
		}
	}
}

func TestDecryptSecret_RejectsUnversionedAndTamperedCiphertext(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, strings.Repeat("a1b2c3d4", 8))

	if _, err := DecryptSecret("plain-text-not-ours"); err == nil {
		t.Fatal("missing version prefix must error so an unencrypted row throws instead of silently returning gibberish")
	}

	ct, err := EncryptSecret("hello")
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte after the prefix to simulate tampering.
	replacement := "X"
	if ct[3:4] == replacement {
		replacement = "Y"
	}
	tampered := ct[:3] + replacement + ct[4:]
	if _, err := DecryptSecret(tampered); err == nil {
		t.Fatal("tampered ciphertext must fail authenticated decrypt")
	}
}

func TestEncryptSecret_RequiresEnvVar(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "")
	if _, err := EncryptSecret("anything"); err == nil {
		t.Fatal("missing env var must surface as ErrSecretKeyMissing")
	}
}
