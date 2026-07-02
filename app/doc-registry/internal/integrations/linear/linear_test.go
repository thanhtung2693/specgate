package linear

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// withLinearGraphQL points the package's Linear endpoint at a test server for
// the duration of a (non-parallel) test and restores it afterward.
func withLinearGraphQL(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev := GraphQLURL
	GraphQLURL = srv.URL
	t.Cleanup(func() { GraphQLURL = prev })
}

// An Issue webhook must normalize onto the shared delivery shape: the LOY-128
// identifier, the started workflow state.type carried raw, the webhookId as the
// dedup key, and no MR-shaped deliveryState. The `fixes SPECGATE-{key}` correlation
// footer rides through on the Description for the parent's parseFixesRefs.
func TestLinearNormalizedDelivery_MapsFields(t *testing.T) {
	t.Parallel()
	raw := `{"type":"Issue","webhookId":"9c8b7a65-4321-4abc-9def-0123456789ab",` +
		`"data":{"id":"i-1","identifier":"LOY-128","title":"Loyalty tweak",` +
		`"description":"do the thing\n\nfixes SPECGATE-CR-LOYALTY-V1","url":"https://linear.app/LOY-128",` +
		`"state":{"id":"s1","name":"In Progress","type":"started"}}}`
	nd, err := ParseAndNormalize(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if nd.Provider != coretypes.ProviderLinear || nd.EventType != coretypes.WebhookEventTrackerIssue {
		t.Fatalf("provider/eventType = %q/%q", nd.Provider, nd.EventType)
	}
	if nd.ExternalKey != "LOY-128" {
		t.Fatalf("identifier = %q, want LOY-128", nd.ExternalKey)
	}
	if nd.RawState != "started" {
		t.Fatalf("RawState = %q, want \"started\" (state.type)", nd.RawState)
	}
	// Action carries the full human-readable state name (state.name).
	if nd.Action != "In Progress" {
		t.Fatalf("Action = %q, want \"In Progress\" (state.name)", nd.Action)
	}
	// The contract says tracker state does NOT map onto MR delivery states.
	if nd.DeliveryState != "" {
		t.Fatalf("deliveryState = %q, want empty (tracker state is not MR-shaped)", nd.DeliveryState)
	}
	if nd.ExternalEventID != "9c8b7a65-4321-4abc-9def-0123456789ab" {
		t.Fatalf("externalEventID = %q, want the webhookId", nd.ExternalEventID)
	}
	if !strings.Contains(nd.Description, "fixes SPECGATE-CR-LOYALTY-V1") {
		t.Fatalf("description = %q, want it to carry the fixes footer", nd.Description)
	}
}

// ParseAndNormalize with state.type="started" and state.name="In Review" must
// produce Action="In Review" and RawState="started". This is the core contract
// for the TrackerLink.TrackerState change: the full state name is stored.
func TestLinearNormalizedDelivery_ActionIsFullStateName(t *testing.T) {
	t.Parallel()
	raw := `{"type":"Issue","webhookId":"wh-abc",` +
		`"data":{"id":"i-2","identifier":"ZOP-9","title":"Zop tweak",` +
		`"url":"https://linear.app/ZOP-9",` +
		`"state":{"id":"s2","name":"In Review","type":"started"}}}`
	nd, err := ParseAndNormalize(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if nd.Action != "In Review" {
		t.Fatalf("Action = %q, want \"In Review\" (state.name)", nd.Action)
	}
	if nd.RawState != "started" {
		t.Fatalf("RawState = %q, want \"started\" (state.type)", nd.RawState)
	}
}

// A non-Issue event normalizes to an empty EventType so the parent ignores it.
func TestParseAndNormalize_NonIssueIsIgnored(t *testing.T) {
	t.Parallel()
	nd, err := ParseAndNormalize(`{"type":"Comment"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if nd.EventType != "" {
		t.Fatalf("eventType = %q, want empty (non-Issue is ignored)", nd.EventType)
	}
}

func TestParseAndNormalize_MissingTypeIsValidationError(t *testing.T) {
	t.Parallel()
	if _, err := ParseAndNormalize(`{}`); !errors.Is(err, coretypes.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// ParseAndNormalize must carry the Linear priority field through to the
// NormalizedDelivery so createTrackerFeedback can embed it in payload_json for
// the stale-warning check.
func TestParseAndNormalize_ExtractsPriority(t *testing.T) {
	t.Parallel()
	raw := `{"type":"Issue","webhookId":"abc-123",` +
		`"data":{"id":"i-2","identifier":"LOY-200","title":"Urgent work","description":"fix it","url":"https://linear.app/LOY-200",` +
		`"priority":1,` +
		`"state":{"id":"s1","name":"In Progress","type":"started"}}}`
	nd, err := ParseAndNormalize(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if nd.Priority != 1 {
		t.Fatalf("Priority = %d, want 1 (urgent)", nd.Priority)
	}
}

// A payload with no priority field (zero value) must parse without error and
// yield Priority=0 (no priority set), so the warning is not triggered for
// unprioritized issues.
func TestParseAndNormalize_NoPriorityFieldIsZero(t *testing.T) {
	t.Parallel()
	raw := `{"type":"Issue","webhookId":"abc-456",` +
		`"data":{"id":"i-3","identifier":"LOY-201","title":"Normal work","description":"","url":"https://linear.app/LOY-201",` +
		`"state":{"id":"s2","name":"Todo","type":"unstarted"}}}`
	nd, err := ParseAndNormalize(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if nd.Priority != 0 {
		t.Fatalf("Priority = %d, want 0 (absent/no priority)", nd.Priority)
	}
}

func TestCreateLinearIssueViaGraphQL_Success(t *testing.T) {
	var gotAuth, gotBody string
	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"i1","identifier":"ENG-123","url":"https://linear.app/ENG-123"}}}}`)
	})

	issue, err := createLinearIssueViaGraphQL(context.Background(), "lin_token_x", false, "team-uuid", "Title", "Body")
	if err != nil {
		t.Fatal(err)
	}
	if issue.Identifier != "ENG-123" || issue.URL != "https://linear.app/ENG-123" {
		t.Fatalf("issue = %#v", issue)
	}
	if gotAuth != "lin_token_x" {
		t.Fatalf("Authorization = %q, want bare token (no Bearer)", gotAuth)
	}
	if !strings.Contains(gotBody, `"teamId":"team-uuid"`) {
		t.Fatalf("request body missing teamId: %s", gotBody)
	}
}

// An OAuth access token must travel as `Authorization: Bearer <token>`; sending
// it bare (the personal-API-key convention) yields 401 from Linear.
func TestCreateLinearIssueViaGraphQL_OAuthUsesBearer(t *testing.T) {
	var gotAuth string
	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"i1","identifier":"ENG-1","url":"u"}}}}`)
	})

	if _, err := createLinearIssueViaGraphQL(context.Background(), "oauth_tok", true, "team-uuid", "T", "B"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer oauth_tok" {
		t.Fatalf("Authorization = %q, want \"Bearer oauth_tok\"", gotAuth)
	}
}

func TestCreateLinearIssueViaGraphQL_GraphQLErrorIsUpstream(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"invalid token"}]}`)
	})
	_, err := createLinearIssueViaGraphQL(context.Background(), "bad", false, "team-uuid", "T", "B")
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
}

func TestCreateLinearIssueViaGraphQL_Non2xxIsUpstream(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := createLinearIssueViaGraphQL(context.Background(), "tok", false, "team", "T", "B")
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
}

func TestCreateLinearIssueViaGraphQL_NoTeamIsValidationError(t *testing.T) {
	t.Parallel()
	_, err := createLinearIssueViaGraphQL(context.Background(), "tok", false, "  ", "T", "B")
	if !errors.Is(err, coretypes.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestResolveLinearTeamViaGraphQL_FirstWhenNoKey(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"teams":{"nodes":[{"id":"t1","key":"ENG"},{"id":"t2","key":"OPS"}]}}}`)
	})
	id, err := resolveLinearTeamViaGraphQL(context.Background(), "tok", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if id != "t1" {
		t.Fatalf("team id = %q, want first team t1", id)
	}
}

func TestResolveLinearTeamViaGraphQL_MatchesKeyCaseInsensitive(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"teams":{"nodes":[{"id":"t1","key":"ENG"},{"id":"t2","key":"OPS"}]}}}`)
	})
	id, err := resolveLinearTeamViaGraphQL(context.Background(), "tok", false, "ops")
	if err != nil {
		t.Fatal(err)
	}
	if id != "t2" {
		t.Fatalf("team id = %q, want key-matched t2", id)
	}
}

func TestResolveLinearTeamViaGraphQL_NoTeamsIsValidationError(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"teams":{"nodes":[]}}}`)
	})
	_, err := resolveLinearTeamViaGraphQL(context.Background(), "tok", false, "")
	if !errors.Is(err, coretypes.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// GetCompletedStateID returns the id of the first state with type="completed".
func TestGetCompletedStateID_ReturnsFirstCompleted(t *testing.T) {
	var gotBody string
	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"data":{"team":{"states":{"nodes":[`+
			`{"id":"s1","name":"Todo","type":"unstarted"},`+
			`{"id":"s2","name":"In Progress","type":"started"},`+
			`{"id":"s3","name":"Done","type":"completed"},`+
			`{"id":"s4","name":"Cancelled","type":"canceled"}`+
			`]}}}}`)
	})
	id, err := GetCompletedStateID(context.Background(), "tok", false, "team-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if id != "s3" {
		t.Fatalf("stateID = %q, want s3 (first completed state)", id)
	}
	if !strings.Contains(gotBody, `"teamId":"team-uuid"`) {
		t.Fatalf("request missing teamId variable: %s", gotBody)
	}
}

// GetCompletedStateID returns ErrUpstream when no state has type="completed".
func TestGetCompletedStateID_NoCompletedStateIsUpstream(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"team":{"states":{"nodes":[{"id":"s1","name":"Todo","type":"unstarted"}]}}}}`)
	})
	_, err := GetCompletedStateID(context.Background(), "tok", false, "team-uuid")
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
}

// GetCompletedStateID returns ErrValidation for an empty teamID.
func TestGetCompletedStateID_EmptyTeamIDIsValidation(t *testing.T) {
	t.Parallel()
	_, err := GetCompletedStateID(context.Background(), "tok", false, "  ")
	if !errors.Is(err, coretypes.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TransitionIssueToState sends the mutation and succeeds on success=true.
func TestTransitionIssueToState_Success(t *testing.T) {
	var gotBody string
	withLinearGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"data":{"issueUpdate":{"success":true,"issue":{"id":"i1","state":{"name":"Done","type":"completed"}}}}}`)
	})
	err := TransitionIssueToState(context.Background(), "tok", false, "i1", "state-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"id":"i1"`) {
		t.Fatalf("request missing issue id: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"stateId":"state-uuid"`) {
		t.Fatalf("request missing stateId: %s", gotBody)
	}
}

// TransitionIssueToState propagates ErrUpstream on a GraphQL error.
func TestTransitionIssueToState_GraphQLErrorIsUpstream(t *testing.T) {
	withLinearGraphQL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"not found"}]}`)
	})
	err := TransitionIssueToState(context.Background(), "tok", false, "i1", "s1")
	if !errors.Is(err, coretypes.ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
}

// TransitionIssueToState returns ErrValidation for empty issueID or stateID.
func TestTransitionIssueToState_EmptyInputsAreValidation(t *testing.T) {
	t.Parallel()
	if err := TransitionIssueToState(context.Background(), "tok", false, "", "s1"); !errors.Is(err, coretypes.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for empty issueID", err)
	}
	if err := TransitionIssueToState(context.Background(), "tok", false, "i1", ""); !errors.Is(err, coretypes.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for empty stateID", err)
	}
}
