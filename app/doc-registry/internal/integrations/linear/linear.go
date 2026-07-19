// Package linear holds Service-independent Linear GraphQL and webhook helpers.
package linear

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// GraphQLURL is the Linear GraphQL endpoint. It is a package var (not a const)
// so tests can repoint it at an httptest server and exercise the real client
// end-to-end.
var GraphQLURL = "https://api.linear.app/graphql"

const maxGraphQLResponseBytes = 4 << 20

const issueCreateMutation = `mutation IssueCreate($input: IssueCreateInput!) {
  issueCreate(input: $input) { success issue { id identifier url team { id } } }
}`

const issueQuery = `query Issue($id: String!) {
  issue(id: $id) { id identifier url team { id } }
}`

const teamProjectsQuery = `query TeamProjects($id: String!) {
  team(id: $id) {
    projects {
      nodes {
        id
        name
        slugId
      }
    }
  }
}`

type TeamSummary struct {
	ID   string
	Key  string
	Name string
}

type ProjectSummary struct {
	ID   string
	Name string
	Slug string
}

// IssueInput is the exact caller-selected shape accepted by Linear's
// IssueCreateInput. The handoff service supplies a stable UUID-shaped ID so a
// retry can discover an issue after an ambiguous create response.
type IssueInput struct {
	ID          string
	TeamID      string
	Title       string
	Description string
}

// Issue is the provider identity persisted by the handoff service.
type Issue struct {
	ID         string
	Identifier string
	URL        string
	TeamID     string `json:"-"`
}

func (i *Issue) UnmarshalJSON(raw []byte) error {
	var value struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
		URL        string `json:"url"`
		Team       *struct {
			ID string `json:"id"`
		} `json:"team"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*i = Issue{ID: value.ID, Identifier: value.Identifier, URL: value.URL}
	if value.Team != nil {
		i.TeamID = value.Team.ID
	}
	return nil
}

type ambiguousCreateError struct{ err error }

func (e ambiguousCreateError) Error() string { return e.err.Error() }
func (e ambiguousCreateError) Unwrap() error { return e.err }

// IsAmbiguousCreateError reports whether Linear may have committed an issue
// despite the response being lost, malformed, or incomplete.
func IsAmbiguousCreateError(err error) bool {
	var ambiguous ambiguousCreateError
	return errors.As(err, &ambiguous)
}

type definiteRequestError struct{ err error }

func (e definiteRequestError) Error() string { return e.err.Error() }
func (e definiteRequestError) Unwrap() error { return e.err }

func isDefiniteRequestError(err error) bool {
	var definite definiteRequestError
	return errors.As(err, &definite)
}

type graphqlResponseError struct{ detail []byte }

func (e graphqlResponseError) Error() string {
	return fmt.Sprintf("%v: linear graphql errors: %s", coretypes.ErrUpstream, string(e.detail))
}

func (e graphqlResponseError) Unwrap() error { return coretypes.ErrUpstream }

func isGraphQLNotFoundError(err error) bool {
	var graphErr graphqlResponseError
	if !errors.As(err, &graphErr) {
		return false
	}
	detail := strings.ToLower(string(graphErr.detail))
	return strings.Contains(detail, "not found") || strings.Contains(detail, "does not exist")
}

// ParseAndNormalize parses a raw Linear webhook body and maps an Issue event
// onto the provider-neutral coretypes.NormalizedDelivery. The webhook's own
// webhookId is the dedup key (a body hash falls back when absent). A non-Issue
// event returns an empty EventType so the parent ignores it; a parse failure
// (bad JSON, missing type) is an error. The raw payload struct never crosses the
// package boundary.
func ParseAndNormalize(raw string) (coretypes.NormalizedDelivery, error) {
	payload, err := parseLinearWebhookPayload(raw)
	if err != nil {
		return coretypes.NormalizedDelivery{}, err
	}
	if !strings.EqualFold(payload.Type, "Issue") {
		// Unsupported event: linearNormalizedDelivery always sets a non-empty
		// EventType, so an empty EventType uniquely means "ignore" to the parent.
		return coretypes.NormalizedDelivery{}, nil
	}
	return linearNormalizedDelivery(payload, raw), nil
}

// ParseCommentScopeDrift parses a Linear Comment webhook into the shared
// comment shape used for scope-drift feedback ingestion.
func ParseCommentScopeDrift(raw string) (coretypes.NormalizedComment, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return coretypes.NormalizedComment{}, fmt.Errorf("%w: invalid linear comment payload", coretypes.ErrValidation)
	}
	if !strings.EqualFold(strings.TrimSpace(asString(payload["type"])), "Comment") {
		return coretypes.NormalizedComment{}, fmt.Errorf("%w: unsupported linear comment payload", coretypes.ErrValidation)
	}
	data, _ := payload["data"].(map[string]any)
	issue, _ := data["issue"].(map[string]any)
	body := strings.TrimSpace(asString(data["body"]))
	identifier := strings.TrimSpace(asString(issue["identifier"]))
	title := strings.TrimSpace(asString(issue["title"]))
	url := strings.TrimSpace(asString(data["url"]))
	if url == "" {
		url = strings.TrimSpace(asString(issue["url"]))
	}
	author := ""
	if user, _ := data["user"].(map[string]any); user != nil {
		author = strings.TrimSpace(asString(user["name"]))
		if author == "" {
			author = strings.TrimSpace(asString(user["displayName"]))
		}
	}
	externalEventID := strings.TrimSpace(asString(payload["webhookId"]))
	if externalEventID == "" {
		sum := sha256.Sum256([]byte(raw))
		externalEventID = "sha256:" + hex.EncodeToString(sum[:])
	}
	return coretypes.NormalizedComment{
		Provider:        coretypes.ProviderLinear,
		EventType:       coretypes.WebhookEventComment,
		ExternalEventID: externalEventID,
		RawPayload:      raw,
		ExternalID:      strings.TrimSpace(asString(data["id"])),
		ExternalKey:     identifier,
		Title:           title,
		Body:            body,
		URL:             url,
		Author:          author,
	}, nil
}

// linearNormalizedDelivery maps the Linear Issue webhook onto the shared
// NormalizedDelivery shape. The tracker workflow state.type is carried in
// rawState as-is (triage|backlog|unstarted|started|completed|canceled): the
// handoff contract states it does not map onto the MR-shaped delivery states,
// so deliveryState is left empty.
func linearNormalizedDelivery(payload linearWebhookPayload, raw string) coretypes.NormalizedDelivery {
	externalEventID := strings.TrimSpace(payload.WebhookID)
	if externalEventID == "" {
		sum := sha256.Sum256([]byte(raw))
		externalEventID = "sha256:" + hex.EncodeToString(sum[:])
	}
	rawState := strings.ToLower(strings.TrimSpace(payload.Data.State.Type))
	return coretypes.NormalizedDelivery{
		Provider:         coretypes.ProviderLinear,
		EventType:        coretypes.WebhookEventTrackerIssue,
		ExternalEventID:  externalEventID,
		RawPayload:       raw,
		ExternalID:       strings.TrimSpace(payload.Data.ID),
		ExternalKey:      strings.TrimSpace(payload.Data.Identifier),
		Title:            payload.Data.Title,
		Description:      payload.Data.Description,
		URL:              payload.Data.URL,
		Action:           strings.TrimSpace(payload.Data.State.Name),
		RawState:         rawState,
		TrackerLifecycle: linearTrackerLifecycle(rawState),
		Priority:         payload.Data.Priority,
	}
}

func linearTrackerLifecycle(stateType string) string {
	switch stateType {
	case "completed", "canceled":
		return coretypes.TrackerLifecycleClosed
	default:
		return coretypes.TrackerLifecycleOpened
	}
}

type linearWebhookPayload struct {
	Action    string `json:"action"`
	Type      string `json:"type"`
	WebhookID string `json:"webhookId"`
	Data      struct {
		ID          string `json:"id"`
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
		State       struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"state"`
		// Priority is the Linear issue priority: 0 = no priority, 1 = urgent,
		// 2 = high, 3 = normal, 4 = low. Carried through to createTrackerFeedback
		// so stale-warning logic can surface urgent/high-priority unhandoff'd CRs.
		Priority int `json:"priority"`
	} `json:"data"`
}

func parseLinearWebhookPayload(raw string) (linearWebhookPayload, error) {
	var payload linearWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("%w: invalid linear webhook payload", coretypes.ErrValidation)
	}
	payload.Type = strings.TrimSpace(payload.Type)
	payload.Data.Identifier = strings.TrimSpace(payload.Data.Identifier)
	if payload.Type == "" {
		return payload, fmt.Errorf("%w: linear webhook type is required", coretypes.ErrValidation)
	}
	return payload, nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// linearGraphQLRequest POSTs a GraphQL document to Linear. The auth header
// depends on the credential type: an OAuth access token must carry the `Bearer`
// prefix, while a personal API key is sent bare (the Linear convention). Passing
// an OAuth token without Bearer yields 401. Transport errors, a non-2xx status,
// and a GraphQL `errors` array all map to ErrUpstream so the HTTP layer can
// surface 502 rather than a generic 500.
func linearGraphQLRequest(ctx context.Context, apiToken string, bearer bool, query string, variables map[string]any, out any) error {
	payload := map[string]any{"query": query}
	if variables != nil {
		payload["variables"] = variables
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return definiteRequestError{err: fmt.Errorf("%w: marshal request: %v", coretypes.ErrUpstream, err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GraphQLURL, bytes.NewReader(buf))
	if err != nil {
		return definiteRequestError{err: fmt.Errorf("%w: build request: %v", coretypes.ErrUpstream, err)}
	}
	if bearer {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	} else {
		req.Header.Set("Authorization", apiToken)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", coretypes.ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("%w: linear returned status %d", coretypes.ErrUpstream, resp.StatusCode)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return definiteRequestError{err: err}
		}
		return err
	}
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	response, err := io.ReadAll(io.LimitReader(resp.Body, maxGraphQLResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: read response: %v", coretypes.ErrUpstream, err)
	}
	if len(response) > maxGraphQLResponseBytes {
		return fmt.Errorf("%w: linear response exceeds %d bytes", coretypes.ErrUpstream, maxGraphQLResponseBytes)
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return fmt.Errorf("%w: decode response: %v", coretypes.ErrUpstream, err)
	}
	if len(envelope.Errors) > 0 {
		detail, _ := json.Marshal(envelope.Errors)
		return definiteRequestError{err: graphqlResponseError{detail: detail}}
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("%w: decode data: %v", coretypes.ErrUpstream, err)
		}
	}
	return nil
}

func CreateIssue(ctx context.Context, apiToken string, bearer bool, in IssueInput) (Issue, error) {
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.TeamID) == "" || strings.TrimSpace(in.Title) == "" {
		return Issue{}, fmt.Errorf("%w: issue id, team id, and title are required", coretypes.ErrValidation)
	}
	var data struct {
		IssueCreate json.RawMessage `json:"issueCreate"`
	}
	variables := map[string]any{"input": map[string]any{"id": in.ID, "teamId": in.TeamID, "title": in.Title, "description": in.Description}}
	if err := linearGraphQLRequest(ctx, apiToken, bearer, issueCreateMutation, variables, &data); err != nil {
		if isDefiniteRequestError(err) {
			return Issue{}, err
		}
		return Issue{}, ambiguousCreateError{err: err}
	}
	if len(data.IssueCreate) == 0 {
		return Issue{}, ambiguousCreateError{err: fmt.Errorf("%w: linear issueCreate response is malformed", coretypes.ErrUpstream)}
	}
	var mutation struct {
		Success *bool  `json:"success"`
		Issue   *Issue `json:"issue"`
	}
	if err := json.Unmarshal(data.IssueCreate, &mutation); err != nil {
		return Issue{}, ambiguousCreateError{err: fmt.Errorf("%w: linear issueCreate response is malformed", coretypes.ErrUpstream)}
	}
	if mutation.Success == nil {
		return Issue{}, ambiguousCreateError{err: fmt.Errorf("%w: linear issueCreate response is incomplete", coretypes.ErrUpstream)}
	}
	if !*mutation.Success {
		return Issue{}, fmt.Errorf("%w: linear issueCreate did not report success", coretypes.ErrUpstream)
	}
	if mutation.Issue == nil || strings.TrimSpace(mutation.Issue.ID) == "" || strings.TrimSpace(mutation.Issue.Identifier) == "" || strings.TrimSpace(mutation.Issue.URL) == "" || strings.TrimSpace(mutation.Issue.TeamID) == "" {
		return Issue{}, ambiguousCreateError{err: fmt.Errorf("%w: linear issueCreate returned an incomplete issue", coretypes.ErrUpstream)}
	}
	return *mutation.Issue, nil
}

// GetIssue returns nil when Linear does not have the caller-selected ID. All
// malformed, GraphQL-error, and non-2xx responses remain ErrUpstream.
func GetIssue(ctx context.Context, apiToken string, bearer bool, id string) (*Issue, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: issue id is required", coretypes.ErrValidation)
	}
	var data struct {
		Issue json.RawMessage `json:"issue"`
	}
	if err := linearGraphQLRequest(ctx, apiToken, bearer, issueQuery, map[string]any{"id": id}, &data); err != nil {
		return nil, err
	}
	if len(data.Issue) == 0 {
		return nil, fmt.Errorf("%w: linear issue lookup response is malformed", coretypes.ErrUpstream)
	}
	if string(data.Issue) == "null" {
		return nil, nil
	}
	var issue Issue
	if err := json.Unmarshal(data.Issue, &issue); err != nil {
		return nil, fmt.Errorf("%w: linear issue lookup response is malformed", coretypes.ErrUpstream)
	}
	if strings.TrimSpace(issue.ID) == "" || strings.TrimSpace(issue.Identifier) == "" || strings.TrimSpace(issue.URL) == "" || strings.TrimSpace(issue.TeamID) == "" {
		return nil, fmt.Errorf("%w: linear issue lookup returned an incomplete issue", coretypes.ErrUpstream)
	}
	return &issue, nil
}

func ListTeamsViaGraphQL(ctx context.Context, apiToken string, bearer bool) ([]TeamSummary, error) {
	var data struct {
		Teams struct {
			Nodes []struct {
				ID   string `json:"id"`
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := linearGraphQLRequest(ctx, apiToken, bearer, `query Teams { teams { nodes { id key name } } }`, nil, &data); err != nil {
		return nil, err
	}
	out := make([]TeamSummary, 0, len(data.Teams.Nodes))
	for _, team := range data.Teams.Nodes {
		out = append(out, TeamSummary{ID: team.ID, Key: team.Key, Name: team.Name})
	}
	return out, nil
}

func ListProjectsForTeamViaGraphQL(ctx context.Context, apiToken string, bearer bool, teamID string) ([]ProjectSummary, error) {
	if strings.TrimSpace(teamID) == "" {
		return nil, fmt.Errorf("%w: linear team id is required", coretypes.ErrValidation)
	}
	var data struct {
		Team *struct {
			Projects struct {
				Nodes []struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					SlugID string `json:"slugId"`
				} `json:"nodes"`
			} `json:"projects"`
		} `json:"team"`
	}
	if err := linearGraphQLRequest(ctx, apiToken, bearer, teamProjectsQuery, map[string]any{"id": teamID}, &data); err != nil {
		return nil, err
	}
	if data.Team == nil {
		return nil, fmt.Errorf("%w: linear team %q not found", coretypes.ErrValidation, teamID)
	}
	out := make([]ProjectSummary, 0, len(data.Team.Projects.Nodes))
	for _, project := range data.Team.Projects.Nodes {
		out = append(out, ProjectSummary{ID: project.ID, Name: project.Name, Slug: project.SlugID})
	}
	return out, nil
}

// teamStatesQuery fetches all workflow states for a team so the caller can pick
// the first state with type="completed".
const teamStatesQuery = `query TeamStates($teamId: String!) {
  team(id: $teamId) {
    states {
      nodes {
        id
        name
        type
      }
    }
  }
}`

// issueUpdateMutation moves a Linear issue to the given workflow state.
const issueUpdateMutation = `mutation IssueUpdate($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: { stateId: $stateId }) {
    success
    issue { id identifier state { name type } }
  }
}`

// GetCompletedStateID returns the ID of the first workflow state with
// type="completed" for a Linear team. Returns ErrValidation when teamID is
// empty and ErrUpstream on API or GraphQL errors.
func GetCompletedStateID(ctx context.Context, apiToken string, bearer bool, teamID string) (string, error) {
	if strings.TrimSpace(teamID) == "" {
		return "", fmt.Errorf("%w: teamID is required", coretypes.ErrValidation)
	}
	var data struct {
		Team *struct {
			States struct {
				Nodes []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"nodes"`
			} `json:"states"`
		} `json:"team"`
	}
	if err := linearGraphQLRequest(ctx, apiToken, bearer, teamStatesQuery, map[string]any{"teamId": teamID}, &data); err != nil {
		return "", err
	}
	if data.Team == nil {
		return "", fmt.Errorf("%w: linear team %q not found", coretypes.ErrUpstream, teamID)
	}
	for _, n := range data.Team.States.Nodes {
		if strings.ToLower(strings.TrimSpace(n.Type)) == "completed" {
			return n.ID, nil
		}
	}
	return "", fmt.Errorf("%w: no completed state found for linear team %q", coretypes.ErrUpstream, teamID)
}

// TransitionIssueToState moves a Linear issue to the given workflow state.
// Returns ErrValidation for empty inputs and ErrUpstream on API or GraphQL
// errors or when the mutation reports success=false.
func TransitionIssueToState(ctx context.Context, apiToken string, bearer bool, issueID, stateID string) error {
	if strings.TrimSpace(issueID) == "" {
		return fmt.Errorf("%w: issueID is required", coretypes.ErrValidation)
	}
	if strings.TrimSpace(stateID) == "" {
		return fmt.Errorf("%w: stateID is required", coretypes.ErrValidation)
	}
	var data struct {
		IssueUpdate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID    string `json:"id"`
				State struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"state"`
			} `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := linearGraphQLRequest(ctx, apiToken, bearer, issueUpdateMutation, map[string]any{"id": issueID, "stateId": stateID}, &data); err != nil {
		return err
	}
	if !data.IssueUpdate.Success {
		return fmt.Errorf("%w: linear issueUpdate did not report success", coretypes.ErrUpstream)
	}
	return nil
}
