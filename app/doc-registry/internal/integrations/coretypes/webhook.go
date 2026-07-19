package coretypes

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InboundWebhook carries the raw delivery plus the headers a webhook driver needs
// to authenticate and normalize it, provider-neutrally. Each provider reads only
// the subset it cares about (GitLab: Standard Webhooks signature; GitHub/Linear:
// signature).
type InboundWebhook struct {
	EventHeader string // X-Gitlab-Event / X-GitHub-Event
	EventUUID   string // X-Gitlab-Event-UUID / X-GitHub-Delivery (dedup key)
	// GitLab Standard Webhooks signing-token headers.
	WebhookID        string
	WebhookTimestamp string
	WebhookSignature string
	// Signature is GitHub's X-Hub-Signature-256 or Linear's Linear-Signature.
	Signature   string
	PayloadJSON string
}

// ProviderTarget identifies the provider repo/project a managed webhook lives on
// and the outbound credential used to manage it. Fields are raw (the integration
// base_url + the resource external id/key); each driver derives its own API root
// and project/repo selector, so the parent needs no provider switch to build it.
type ProviderTarget struct {
	BaseURL     string // integration.BaseURL (raw)
	Token       string // decrypted outbound credential
	Bearer      bool   // OAuth bearer vs PAT/private-token
	ExternalID  string // resource.ExternalID (gitlab numeric project id)
	ExternalKey string // resource.ExternalKey (gitlab path / github owner/repo)
}

// ProvisionInput asks a driver to register a managed webhook for a resource.
type ProvisionInput struct {
	Target     ProviderTarget
	WebhookURL string
}

// ProvisionResult is the registered provider hook id plus the secret the parent
// must store (encrypted) for inbound verification.
type ProvisionResult struct {
	ProviderHookID string
	Secret         string
}

// WebhookDriver is the per-provider seam for inbound webhook auth + normalization
// and managed-webhook provisioning. Each provider package registers one from its
// init() via RegisterWebhookDriver. The parent resolves/decrypts secrets and runs
// the shared DB-commit pipeline; the driver owns everything provider-specific.
type WebhookDriver interface {
	// VerifyDelivery authenticates the raw delivery against the plaintext secret.
	// Returns nil when authentic, ErrUnauthorized otherwise.
	VerifyDelivery(secret string, in InboundWebhook) error
	// Normalize classifies + parses the delivery. On success exactly one of
	// nd/comment is non-nil; a non-empty ignoreReason means "200 ignored".
	Normalize(in InboundWebhook) (nd *NormalizedDelivery, comment *NormalizedComment, ignoreReason string, err error)
	// SupportsManagedWebhook reports whether ProvisionWebhook/DeleteWebhook are
	// implemented (GitLab/GitHub/Linear yes).
	SupportsManagedWebhook() bool
	ProvisionWebhook(ctx context.Context, in ProvisionInput) (ProvisionResult, error)
	DeleteWebhook(ctx context.Context, hookID string, target ProviderTarget) error
}

var (
	webhookMu      sync.RWMutex
	webhookDrivers = map[string]WebhookDriver{}
)

// RegisterWebhookDriver wires a provider's webhook driver into the registry. Each
// provider package calls this from init() — the single place a provider is added
// to the inbound-webhook path.
func RegisterWebhookDriver(provider string, d WebhookDriver) {
	webhookMu.Lock()
	defer webhookMu.Unlock()
	webhookDrivers[provider] = d
}

// LookupWebhookDriver returns the driver registered for a provider, if any.
func LookupWebhookDriver(provider string) (WebhookDriver, bool) {
	webhookMu.RLock()
	defer webhookMu.RUnlock()
	d, ok := webhookDrivers[provider]
	return d, ok
}

// WebhookReplayTolerance bounds how far a GitLab webhook-timestamp may be from
// now (Standard Webhooks replay protection).
const WebhookReplayTolerance = 5 * time.Minute

// WebhookTimestampWithinTolerance reports whether a unix-seconds timestamp string
// is within tolerance of now. A missing/unparseable timestamp is outside
// tolerance so a delivery with no timestamp is refused.
func WebhookTimestampWithinTolerance(timestamp string, tolerance time.Duration) bool {
	secs, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return false
	}
	delta := time.Now().Unix() - secs
	if delta < 0 {
		delta = -delta
	}
	return delta <= int64(tolerance.Seconds())
}

// VerifyGitHubSignature constant-time-checks a GitHub X-Hub-Signature-256 header
// (`sha256=<hex>`) against HMAC-SHA256(secret, body).
func VerifyGitHubSignature(secret string, body []byte, signatureHeader string) bool {
	if secret == "" || signatureHeader == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(signatureHeader, prefix)))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}

const gitLabSigningTokenPrefix = "whsec_"

// IsGitLabSigningToken reports whether a value is a GitLab signing token
// (`whsec_` + base64-encoded 32-byte key).
func IsGitLabSigningToken(token string) bool {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, gitLabSigningTokenPrefix) {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(token, gitLabSigningTokenPrefix))
	return err == nil && len(key) == 32
}

// VerifyGitLabSigningToken validates a GitLab delivery against a `whsec_` signing
// token per the Standard Webhooks spec: HMAC-SHA256 over
// `{webhook-id}.{webhook-timestamp}.{body}`, base64, `v1,`-prefixed, matched
// constant-time against any signature in the space-separated webhook-signature.
func VerifyGitLabSigningToken(signingToken, webhookID, webhookTimestamp string, body []byte, signatureHeader string) bool {
	signingToken = strings.TrimSpace(signingToken)
	webhookID = strings.TrimSpace(webhookID)
	webhookTimestamp = strings.TrimSpace(webhookTimestamp)
	signatureHeader = strings.TrimSpace(signatureHeader)
	if signingToken == "" || webhookID == "" || webhookTimestamp == "" || signatureHeader == "" {
		return false
	}
	if !strings.HasPrefix(signingToken, gitLabSigningTokenPrefix) {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(signingToken, gitLabSigningTokenPrefix))
	if err != nil || len(key) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(webhookID + "." + webhookTimestamp + "."))
	mac.Write(body)
	expected := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, candidate := range strings.Fields(signatureHeader) {
		if subtle.ConstantTimeCompare([]byte(expected), []byte(candidate)) == 1 {
			return true
		}
	}
	return false
}

// VerifyLinearSignature constant-time-checks a Linear-Signature header (bare-hex
// HMAC-SHA256, no `sha256=` prefix) against HMAC-SHA256(secret, body).
func VerifyLinearSignature(secret string, body []byte, signatureHeader string) bool {
	signatureHeader = strings.TrimSpace(signatureHeader)
	if secret == "" || signatureHeader == "" {
		return false
	}
	want, err := hex.DecodeString(signatureHeader)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}
