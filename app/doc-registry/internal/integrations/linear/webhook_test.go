package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// TestDriverRegistered verifies that init() registered the Linear webhook driver.
// Driver registration is global state, so no parallel guard is needed.
func TestDriverRegistered(t *testing.T) {
	t.Parallel()
	d, ok := coretypes.LookupWebhookDriver(coretypes.ProviderLinear)
	if !ok {
		t.Fatal("Linear webhook driver not registered")
	}
	if !d.SupportsManagedWebhook() {
		t.Error("SupportsManagedWebhook() must return true for Linear")
	}
}

// TestVerifyDelivery_emptySecret verifies that an empty secret is rejected.
func TestVerifyDelivery_emptySecret(t *testing.T) {
	t.Parallel()
	d := webhookDriver{}
	err := d.VerifyDelivery("", coretypes.InboundWebhook{Signature: "abc123", PayloadJSON: `{"type":"Issue"}`})
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
	if !errors.Is(err, coretypes.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got: %v", err)
	}
}

// TestVerifyDelivery_mismatch verifies that a wrong signature is rejected.
func TestVerifyDelivery_mismatch(t *testing.T) {
	t.Parallel()
	d := webhookDriver{}
	err := d.VerifyDelivery("my-secret", coretypes.InboundWebhook{Signature: "deadbeef", PayloadJSON: `{"type":"Issue"}`})
	if err == nil {
		t.Error("expected error for wrong signature, got nil")
	}
	if !errors.Is(err, coretypes.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got: %v", err)
	}
}

// TestProvisionWebhook_missingTeamID verifies that an empty ExternalID is
// rejected with ErrValidation before any API call is made.
// This does not touch GraphQLURL so it is safe to run in parallel.
func TestProvisionWebhook_missingTeamID(t *testing.T) {
	t.Parallel()

	d := webhookDriver{}
	_, err := d.ProvisionWebhook(context.Background(), coretypes.ProvisionInput{
		Target:     coretypes.ProviderTarget{Token: "tok", ExternalID: ""},
		WebhookURL: "https://example.com/webhook",
	})
	if err == nil {
		t.Fatal("expected validation error for empty ExternalID")
	}
	if !errors.Is(err, coretypes.ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", err)
	}
}

// TestDeleteWebhook_emptyID verifies that an empty hook ID is rejected before
// any API call. Does not touch GraphQLURL so it is safe to run in parallel.
func TestDeleteWebhook_emptyID(t *testing.T) {
	t.Parallel()

	d := webhookDriver{}
	err := d.DeleteWebhook(context.Background(), "", coretypes.ProviderTarget{Token: "tok"})
	if err == nil {
		t.Fatal("expected validation error for empty hook ID")
	}
	if !errors.Is(err, coretypes.ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", err)
	}
}

// The tests below repoint the package-level GraphQLURL, so they must NOT run
// in parallel (see withLinearGraphQL doc comment in linear_test.go).

// TestProvisionWebhook_success verifies that ProvisionWebhook calls the
// webhookCreate mutation with the correct fields and returns a populated result.
func TestProvisionWebhook_success(t *testing.T) {
	const wantHookID = "wh-abc123"
	const wantTeamID = "team-uuid-42"
	const wantURL = "https://registry.example.com/integrations/int-1/resources/res-1/linear/webhook"

	var gotInput map[string]any

	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		gotInput = req.Variables.Input
		resp := map[string]any{
			"data": map[string]any{
				"webhookCreate": map[string]any{
					"success": true,
					"webhook": map[string]any{
						"id":      wantHookID,
						"url":     wantURL,
						"enabled": true,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	d := webhookDriver{}
	result, err := d.ProvisionWebhook(context.Background(), coretypes.ProvisionInput{
		Target: coretypes.ProviderTarget{
			Token:      "lin_api_token",
			Bearer:     false,
			ExternalID: wantTeamID,
		},
		WebhookURL: wantURL,
	})
	if err != nil {
		t.Fatalf("ProvisionWebhook returned unexpected error: %v", err)
	}

	if result.ProviderHookID != wantHookID {
		t.Errorf("ProviderHookID = %q, want %q", result.ProviderHookID, wantHookID)
	}
	// Secret must be a 64-char hex string (32 bytes).
	if len(result.Secret) != 64 {
		t.Errorf("Secret length = %d, want 64 (32-byte hex)", len(result.Secret))
	}

	// Verify mutation input fields.
	if gotInput == nil {
		t.Fatal("no input captured from GraphQL request")
	}
	if gotInput["url"] != wantURL {
		t.Errorf("input.url = %q, want %q", gotInput["url"], wantURL)
	}
	if gotInput["teamId"] != wantTeamID {
		t.Errorf("input.teamId = %q, want %q", gotInput["teamId"], wantTeamID)
	}
	secret, _ := gotInput["secret"].(string)
	if secret == "" {
		t.Error("input.secret must be set in the mutation variables")
	}
	if secret != result.Secret {
		t.Errorf("input.secret %q does not match returned result.Secret %q", secret, result.Secret)
	}
	// resourceTypes must contain Issue and Comment.
	rtypes, _ := gotInput["resourceTypes"].([]any)
	if len(rtypes) < 2 {
		t.Errorf("input.resourceTypes = %v, want at least [Issue Comment]", rtypes)
	}
}

// TestProvisionWebhook_upstreamError verifies that an upstream GraphQL error
// is propagated as an ErrUpstream-wrapped error.
func TestProvisionWebhook_upstreamError(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"errors": []map[string]any{
				{"message": "team not found"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	d := webhookDriver{}
	_, err := d.ProvisionWebhook(context.Background(), coretypes.ProvisionInput{
		Target: coretypes.ProviderTarget{
			Token:      "lin_api_token",
			ExternalID: "team-x",
		},
		WebhookURL: "https://example.com/webhook",
	})
	if err == nil {
		t.Fatal("ProvisionWebhook should return an error on upstream GraphQL errors")
	}
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Errorf("expected ErrUpstream, got: %v", err)
	}
}

// TestProvisionWebhook_successFalse verifies that success=false in the
// mutation response yields an ErrUpstream error.
func TestProvisionWebhook_successFalse(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"webhookCreate": map[string]any{
					"success": false,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	d := webhookDriver{}
	_, err := d.ProvisionWebhook(context.Background(), coretypes.ProvisionInput{
		Target: coretypes.ProviderTarget{
			Token:      "lin_api_token",
			ExternalID: "team-x",
		},
		WebhookURL: "https://example.com/webhook",
	})
	if err == nil {
		t.Fatal("ProvisionWebhook should return an error when success=false")
	}
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Errorf("expected ErrUpstream, got: %v", err)
	}
}

// TestDeleteWebhook_success verifies that DeleteWebhook calls the webhookDelete
// mutation with the correct hook ID.
func TestDeleteWebhook_success(t *testing.T) {
	const wantHookID = "wh-to-delete"
	var gotID string

	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				ID string `json:"id"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		gotID = req.Variables.ID
		resp := map[string]any{
			"data": map[string]any{
				"webhookDelete": map[string]any{
					"success": true,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	d := webhookDriver{}
	err := d.DeleteWebhook(context.Background(), wantHookID, coretypes.ProviderTarget{
		Token: "lin_api_token",
	})
	if err != nil {
		t.Fatalf("DeleteWebhook returned unexpected error: %v", err)
	}
	if gotID != wantHookID {
		t.Errorf("variables.id = %q, want %q", gotID, wantHookID)
	}
}

// TestDeleteWebhook_upstreamError verifies that an upstream error is propagated.
func TestDeleteWebhook_upstreamError(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	d := webhookDriver{}
	err := d.DeleteWebhook(context.Background(), "wh-x", coretypes.ProviderTarget{Token: "tok"})
	if err == nil {
		t.Fatal("DeleteWebhook should return error on upstream failure")
	}
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Errorf("expected ErrUpstream, got: %v", err)
	}
}

func TestDeleteWebhook_AlreadyAbsentIsSuccess(t *testing.T) {
	for _, response := range []string{
		`{"data":{"webhookDelete":{"success":false}}}`,
		`{"errors":[{"message":"Entity not found: Webhook"}]}`,
	} {
		t.Run(response, func(t *testing.T) {
			withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, response)
			})
			if err := (webhookDriver{}).DeleteWebhook(context.Background(), "wh-absent", coretypes.ProviderTarget{Token: "tok"}); err != nil {
				t.Fatalf("DeleteWebhook already absent: %v", err)
			}
		})
	}
}

func TestDeleteWebhook_OtherGraphQLErrorStillFails(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"errors":[{"message":"permission denied"}]}`)
	})
	err := (webhookDriver{}).DeleteWebhook(context.Background(), "wh-x", coretypes.ProviderTarget{Token: "tok"})
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Fatalf("DeleteWebhook error = %v, want ErrUpstream", err)
	}
}
