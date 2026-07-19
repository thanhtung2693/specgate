package github

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/githubapi"
	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func init() {
	coretypes.RegisterWebhookDriver(coretypes.ProviderGitHub, webhookDriver{})
}

// webhookDriver is the GitHub implementation of coretypes.WebhookDriver: HMAC
// X-Hub-Signature-256 verification, pull_request / issue_comment normalization,
// and managed repository-webhook provisioning.
type webhookDriver struct{}

func (webhookDriver) VerifyDelivery(secret string, in coretypes.InboundWebhook) error {
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("%w: no github webhook secret configured", coretypes.ErrUnauthorized)
	}
	if !coretypes.VerifyGitHubSignature(secret, []byte(in.PayloadJSON), in.Signature) {
		return fmt.Errorf("%w: github signature mismatch", coretypes.ErrUnauthorized)
	}
	return nil
}

func (webhookDriver) Normalize(in coretypes.InboundWebhook) (*coretypes.NormalizedDelivery, *coretypes.NormalizedComment, string, error) {
	externalEventID := strings.TrimSpace(in.EventUUID)
	if externalEventID == "" {
		externalEventID = payloadHashID(in.PayloadJSON)
	}
	switch strings.ToLower(strings.TrimSpace(in.EventHeader)) {
	case "issue_comment":
		comment, err := ParseCommentScopeDrift(in.PayloadJSON, externalEventID)
		if err != nil {
			return nil, nil, "", err
		}
		return nil, &comment, "", nil
	case "pull_request":
		nd, err := ParseAndNormalize(in.PayloadJSON, externalEventID)
		if err != nil {
			return nil, nil, "", err
		}
		return &nd, nil, "", nil
	default:
		return nil, nil, "unsupported_github_event", nil
	}
}

func (webhookDriver) SupportsManagedWebhook() bool { return true }

func (webhookDriver) ProvisionWebhook(ctx context.Context, in coretypes.ProvisionInput) (coretypes.ProvisionResult, error) {
	secret, err := generateSecret()
	if err != nil {
		return coretypes.ProvisionResult{}, err
	}
	hook, err := newClient(in.Target).CreateRepositoryWebhook(ctx, githubapi.CreateRepositoryWebhookRequest{
		URL:    in.WebhookURL,
		Secret: secret,
	})
	if err != nil {
		return coretypes.ProvisionResult{}, err
	}
	return coretypes.ProvisionResult{ProviderHookID: strconv.Itoa(hook.ID), Secret: secret}, nil
}

func (webhookDriver) DeleteWebhook(ctx context.Context, hookID string, target coretypes.ProviderTarget) error {
	return newClient(target).DeleteRepositoryWebhook(ctx, hookID)
}

func newClient(t coretypes.ProviderTarget) *githubapi.Client {
	return githubapi.NewClient(githubapi.ClientConfig{
		APIURL:     githubapi.APIURL(t.BaseURL),
		Token:      t.Token,
		Repo:       strings.TrimSpace(t.ExternalKey),
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	})
}

func generateSecret() (string, error) {
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
