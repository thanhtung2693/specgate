package gitlab

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/gitlabapi"
	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func init() {
	coretypes.RegisterWebhookDriver(coretypes.ProviderGitLab, webhookDriver{})
}

// webhookDriver is the GitLab implementation of coretypes.WebhookDriver: it
// authenticates inbound deliveries (signing token or legacy secret token),
// normalizes them, and provisions/deletes the managed project webhook.
type webhookDriver struct{}

// VerifyDelivery accepts either a whsec_ signing token (Standard Webhooks
// signature + timestamp recency, GitLab 19.0+) or a legacy secret token
// (verbatim X-Gitlab-Token), chosen by the stored secret's shape.
func (webhookDriver) VerifyDelivery(secret string, in coretypes.InboundWebhook) error {
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("%w: no gitlab webhook secret configured", coretypes.ErrUnauthorized)
	}
	if coretypes.IsGitLabSigningToken(secret) {
		if !coretypes.WebhookTimestampWithinTolerance(in.WebhookTimestamp, coretypes.WebhookReplayTolerance) {
			return fmt.Errorf("%w: gitlab webhook timestamp outside tolerance", coretypes.ErrUnauthorized)
		}
		if !coretypes.VerifyGitLabSigningToken(secret, in.WebhookID, in.WebhookTimestamp, []byte(in.PayloadJSON), in.WebhookSignature) {
			return fmt.Errorf("%w: gitlab webhook signature mismatch", coretypes.ErrUnauthorized)
		}
		return nil
	}
	if !coretypes.VerifyWebhookSecret(coretypes.HashWebhookSecret(secret), in.Token) {
		return fmt.Errorf("%w: gitlab webhook token mismatch", coretypes.ErrUnauthorized)
	}
	return nil
}

// Normalize routes Note Hooks to comment scope drift and everything else through
// ParseAndNormalize, keeping only merge-request and tracker-issue events.
func (webhookDriver) Normalize(in coretypes.InboundWebhook) (*coretypes.NormalizedDelivery, *coretypes.NormalizedComment, string, error) {
	externalEventID := strings.TrimSpace(in.EventUUID)
	if externalEventID == "" {
		externalEventID = payloadHashID(in.PayloadJSON)
	}
	if strings.ToLower(strings.TrimSpace(in.EventHeader)) == "note hook" {
		comment, err := ParseCommentScopeDrift(in.PayloadJSON, externalEventID)
		if err != nil {
			return nil, nil, "", err
		}
		return nil, &comment, "", nil
	}
	nd, err := ParseAndNormalize(in.PayloadJSON, externalEventID)
	if err != nil {
		return nil, nil, "", err
	}
	if nd.EventType != coretypes.WebhookEventMergeRequest && nd.EventType != coretypes.WebhookEventTrackerIssue {
		return nil, nil, "unsupported_gitlab_event", nil
	}
	return &nd, nil, "", nil
}

func (webhookDriver) SupportsManagedWebhook() bool { return true }

// ProvisionWebhook registers a project webhook with BOTH a whsec_ signing token
// and a legacy secret token, then returns whichever GitLab actually stored
// (signing_token_present) so verification matches. GitLab < 19.0 ignores the
// signing token and keeps the secret token.
func (webhookDriver) ProvisionWebhook(ctx context.Context, in coretypes.ProvisionInput) (coretypes.ProvisionResult, error) {
	signingToken, err := generateSigningToken()
	if err != nil {
		return coretypes.ProvisionResult{}, err
	}
	secretToken, err := generateSecretToken()
	if err != nil {
		return coretypes.ProvisionResult{}, err
	}
	hook, err := newClient(in.Target).CreateProjectWebhook(ctx, gitlabapi.CreateProjectWebhookRequest{
		URL:          in.WebhookURL,
		SigningToken: signingToken,
		SecretToken:  secretToken,
	})
	if err != nil {
		return coretypes.ProvisionResult{}, err
	}
	secret := signingToken
	if !hook.SigningTokenPresent {
		secret = secretToken
	}
	return coretypes.ProvisionResult{ProviderHookID: strconv.Itoa(hook.ID), Secret: secret}, nil
}

func (webhookDriver) DeleteWebhook(ctx context.Context, hookID string, target coretypes.ProviderTarget) error {
	return newClient(target).DeleteProjectWebhook(ctx, hookID)
}

func newClient(t coretypes.ProviderTarget) *gitlabapi.Client {
	projectID := strings.TrimSpace(t.ExternalID)
	if projectID == "" {
		projectID = strings.TrimSpace(t.ExternalKey)
	}
	return gitlabapi.NewClient(gitlabapi.ClientConfig{
		APIURL:     gitLabAPIURL(t.BaseURL),
		Token:      t.Token,
		ProjectID:  projectID,
		Bearer:     t.Bearer,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	})
}

// generateSigningToken returns a Standard Webhooks signing token: whsec_ + a
// base64-encoded random 32-byte key.
func generateSigningToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(raw[:]), nil
}

// generateSecretToken returns a random 32-byte hex secret (the legacy
// X-Gitlab-Token value).
func generateSecretToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func payloadHashID(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}
