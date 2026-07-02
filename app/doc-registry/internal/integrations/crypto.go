package integrations

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// SecretKeyEnvVar names the env var holding the AES-256 master key used for
// EncryptSecret/DecryptSecret. Value must be 32 raw bytes hex-encoded
// (64 hex chars). Generate once with `openssl rand -hex 32`.
//
// This provides field-level encryption for sensitive credential values —
// strong enough to defeat "anyone with read access to the DB lifts every
// credential" but weaker than KMS envelope encryption (the master
// key sits in the process address space; rotation requires re-encrypting
// every value). Upgrade to KMS-backed envelope encryption before scaling
// past a single deployment.
const SecretKeyEnvVar = "DOC_REGISTRY_SECRET_KEY"

// SettingsSecretKeyEnvVar is the shared settings encryption key (same
// 32-byte-hex format). loadSecretKey falls back to it when SecretKeyEnvVar is
// unset, so a deployment can run on a single master key.
const SettingsSecretKeyEnvVar = "SETTINGS_ENCRYPTION_KEY"

// secretCiphertextPrefix marks AES-GCM ciphertext rows so a DB row inserted
// by an older or unencrypted writer cannot be mistaken for ciphertext on
// read (the absence of the prefix tells DecryptSecret to fail loudly
// instead of silently returning corrupted bytes).
const secretCiphertextPrefix = "v1:"

var (
	ErrSecretKeyMissing = errors.New("integration secret key not configured (set " + SecretKeyEnvVar + " or " + SettingsSecretKeyEnvVar + ")")
	ErrSecretCiphertext = errors.New("integration secret ciphertext is malformed")
)

// The inbound-webhook verification helpers live in coretypes so the per-provider
// webhook drivers (which cannot import this parent package) can use them. These
// aliases keep the parent's existing call sites unchanged.
var (
	HashWebhookSecret        = coretypes.HashWebhookSecret
	VerifyWebhookSecret      = coretypes.VerifyWebhookSecret
	VerifyGitHubSignature    = coretypes.VerifyGitHubSignature
	IsGitLabSigningToken     = coretypes.IsGitLabSigningToken
	VerifyGitLabSigningToken = coretypes.VerifyGitLabSigningToken
	VerifyLinearSignature    = coretypes.VerifyLinearSignature
)

func loadSecretKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(SecretKeyEnvVar))
	if raw == "" {
		// Reuse the shared settings encryption key so a deployment runs on a
		// single master key instead of managing a second one.
		raw = strings.TrimSpace(os.Getenv(SettingsSecretKeyEnvVar))
	}
	if raw == "" {
		return nil, ErrSecretKeyMissing
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("integration secret key must be 64 hex chars: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("integration secret key must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

// EncryptSecret seals plaintext with AES-256-GCM under the master key in
// SecretKeyEnvVar and returns a portable string `v1:<base64(nonce||ct)>`.
// Use this for any secret the system needs to recover (e.g. a stored
// GitLab Personal Access Token used on outbound API calls). Use
// HashWebhookSecret instead for verify-only secrets — recoverability
// there is a liability, not a feature.
func EncryptSecret(plaintext string) (string, error) {
	key, err := loadSecretKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return secretCiphertextPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// DecryptSecret reverses EncryptSecret. Refuses to decode anything that
// does not carry the `v1:` prefix so an unencrypted-by-mistake row throws
// instead of silently returning gibberish.
func DecryptSecret(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, secretCiphertextPrefix) {
		return "", ErrSecretCiphertext
	}
	raw, err := base64.RawStdEncoding.DecodeString(ciphertext[len(secretCiphertextPrefix):])
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSecretCiphertext, err)
	}
	key, err := loadSecretKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", ErrSecretCiphertext
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSecretCiphertext, err)
	}
	return string(pt), nil
}
