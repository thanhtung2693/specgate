// Package gitlab holds the Service-independent GitLab provider code: the
// outbound tracker adapter and the webhook parse+normalize. It depends only on
// coretypes (leaf domain types/registry) and internal/gitlabapi — never the
// parent integrations package, so there is no import cycle. The parent imports
// this package to trigger adapter registration and delegates its Service
// methods (HandleGitLabWebhook) to the pure functions here.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/gitlabapi"
	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func init() {
	coretypes.RegisterTracker(coretypes.ProviderGitLab, gitlabTracker{})
}

// gitlabTracker resolves the project (a resource_type=project resource or
// config_json.project_id — required, no auto-resolve) and creates the issue via
// the GitLab REST API. apiURL is derived from the integration's base_url.
type gitlabTracker struct{}

func (gitlabTracker) CreateIssue(ctx context.Context, h coretypes.Handoff) (*coretypes.TrackerIssue, error) {
	projectID := explicitGitLabProjectID(h.Integration, h.Resources)
	if projectID == "" {
		return nil, fmt.Errorf("%w: gitlab integration has no project configured (add a resource_type=project resource or config_json.project_id)", coretypes.ErrValidation)
	}
	apiURL := gitLabAPIURL(h.Integration.BaseURL)
	if apiURL == "" {
		return nil, fmt.Errorf("%w: gitlab integration has no base_url for the REST API", coretypes.ErrValidation)
	}
	client := gitlabapi.NewClient(gitlabapi.ClientConfig{
		APIURL:    apiURL,
		Token:     h.APIToken,
		ProjectID: projectID,
		Bearer:    strings.TrimSpace(h.Integration.AuthMethod) == coretypes.AuthMethodOAuth,
	})
	issue, err := client.CreateIssue(ctx, gitlabapi.CreateIssueRequest{
		Title:       h.Title,
		Description: h.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", coretypes.ErrUpstream, err)
	}
	return &coretypes.TrackerIssue{Identifier: fmt.Sprintf("#%d", issue.IID), URL: issue.URL}, nil
}

// explicitGitLabProjectID returns the GitLab project the issue is filed under: a
// resource_type=project resource (external_id numeric id, else external_key
// path) or config_json.project_id, or "" when none is configured.
func explicitGitLabProjectID(integration *coretypes.Integration, resources []coretypes.Resource) string {
	for _, r := range resources {
		if r.ResourceType == coretypes.ResourceTypeProject {
			if id := strings.TrimSpace(r.ExternalID); id != "" {
				return id
			}
			if key := strings.TrimSpace(r.ExternalKey); key != "" {
				return key
			}
		}
	}
	return strings.TrimSpace(coretypes.IntegrationConfigString(integration, "project_id"))
}

// gitLabAPIURL derives the REST API root from the integration's base_url
// (the GitLab host), e.g. https://gitlab.example → https://gitlab.example/api/v4.
func gitLabAPIURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/api/v4"
}

// ParseAndNormalize parses a raw GitLab webhook body, classifies it, and maps it
// onto the provider-neutral coretypes.NormalizedDelivery. externalEventID is the
// dedup key (X-Gitlab-Event-UUID or a body hash) supplied by the caller. The
// returned EventType selects the parent's handling: tracker_issue (an Issue
// Hook, optional tracker augmentation) or merge_request (the delivery floor). An
// empty EventType signals an unsupported object_kind/event_type the parent
// should ignore; a parse failure (bad JSON, missing project identity) is an
// error. The raw payload struct never crosses the package boundary.
func ParseAndNormalize(raw, externalEventID string) (coretypes.NormalizedDelivery, error) {
	payload, err := parseGitLabWebhookPayload(raw)
	if err != nil {
		return coretypes.NormalizedDelivery{}, err
	}
	// GitLab as a tracker: an Issue Hook is the tracker peer of the Linear issue
	// webhook. Classify it before the MR shape and carry tracker_issue so the
	// parent routes it to the augment-only path (no project resource required).
	if payload.ObjectKind == coretypes.WebhookEventIssue || payload.EventType == coretypes.WebhookEventIssue {
		return gitLabIssueNormalizedDelivery(payload, externalEventID, raw), nil
	}
	if payload.ObjectKind == "note" || payload.EventType == "note" {
		return coretypes.NormalizedDelivery{}, nil
	}
	if payload.ObjectKind != coretypes.WebhookEventMergeRequest && payload.EventType != coretypes.WebhookEventMergeRequest {
		// Unsupported kind: both normalizers always set a non-empty EventType, so
		// an empty EventType uniquely means "ignore this event" to the parent.
		return coretypes.NormalizedDelivery{}, nil
	}
	return gitLabNormalizedDelivery(payload, externalEventID, raw), nil
}

// ParseCommentScopeDrift parses a GitLab Note Hook into the provider-neutral
// comment shape used for scope-drift feedback ingestion.
func ParseCommentScopeDrift(raw, externalEventID string) (coretypes.NormalizedComment, error) {
	payload, err := parseGitLabWebhookPayload(raw)
	if err != nil {
		return coretypes.NormalizedComment{}, err
	}
	if payload.ObjectKind != "note" && payload.EventType != "note" {
		return coretypes.NormalizedComment{}, fmt.Errorf("%w: unsupported gitlab note payload", coretypes.ErrValidation)
	}
	title := strings.TrimSpace(payload.MergeRequest.Title)
	if title == "" {
		title = strings.TrimSpace(payload.Issue.Title)
	}
	if title == "" {
		title = strings.TrimSpace(payload.ObjectAttributes.Title)
	}
	return coretypes.NormalizedComment{
		Provider:        coretypes.ProviderGitLab,
		EventType:       coretypes.WebhookEventComment,
		ExternalEventID: externalEventID,
		RawPayload:      raw,
		ProjectID:       payload.Project.ID,
		ProjectKey:      payload.Project.PathWithNamespace,
		ExternalID:      strconv.Itoa(payload.ObjectAttributes.ID),
		ExternalKey:     payload.Project.PathWithNamespace + "#note-" + strconv.Itoa(payload.ObjectAttributes.ID),
		Title:           title,
		Body:            strings.TrimSpace(payload.ObjectAttributes.Note),
		URL:             strings.TrimSpace(payload.ObjectAttributes.URL),
		Author:          strings.TrimSpace(payload.User.Username),
	}, nil
}

func gitLabNormalizedDelivery(payload gitLabWebhookPayload, externalEventID, raw string) coretypes.NormalizedDelivery {
	return coretypes.NormalizedDelivery{
		Provider:        coretypes.ProviderGitLab,
		EventType:       coretypes.WebhookEventMergeRequest,
		ExternalEventID: externalEventID,
		RawPayload:      raw,
		ProjectID:       payload.Project.ID,
		ProjectKey:      payload.Project.PathWithNamespace,
		ExternalID:      strconv.Itoa(payload.ObjectAttributes.ID),
		IID:             payload.ObjectAttributes.IID,
		ExternalKey:     payload.Project.PathWithNamespace + "!" + strconv.Itoa(payload.ObjectAttributes.IID),
		Title:           payload.ObjectAttributes.Title,
		Description:     payload.ObjectAttributes.Description,
		URL:             payload.ObjectAttributes.URL,
		Action:          payload.ObjectAttributes.Action,
		RawState:        payload.ObjectAttributes.State,
		SourceBranch:    payload.ObjectAttributes.SourceBranch,
		TargetBranch:    payload.ObjectAttributes.TargetBranch,
		MergeCommitSHA:  payload.ObjectAttributes.MergeCommitSHA,
		DeliveryState:   gitLabMRDeliveryState(payload.ObjectAttributes.Action, payload.ObjectAttributes.State),
	}
}

// gitLabIssueNormalizedDelivery maps a GitLab Issue Hook onto the shared
// NormalizedDelivery. externalKey is the issue reference (`#42`); rawState is the
// tracker workflow state derived from a scoped label (else open/closed). Like the
// Linear tracker path it carries no MR-shaped deliveryState.
func gitLabIssueNormalizedDelivery(payload gitLabWebhookPayload, externalEventID, raw string) coretypes.NormalizedDelivery {
	return coretypes.NormalizedDelivery{
		Provider:        coretypes.ProviderGitLab,
		EventType:       coretypes.WebhookEventTrackerIssue,
		ExternalEventID: externalEventID,
		RawPayload:      raw,
		ProjectID:       payload.Project.ID,
		ProjectKey:      payload.Project.PathWithNamespace,
		ExternalID:      strconv.Itoa(payload.ObjectAttributes.ID),
		IID:             payload.ObjectAttributes.IID,
		ExternalKey:     "#" + strconv.Itoa(payload.ObjectAttributes.IID),
		Title:           payload.ObjectAttributes.Title,
		Description:     payload.ObjectAttributes.Description,
		URL:             payload.ObjectAttributes.URL,
		Action:          payload.ObjectAttributes.Action,
		RawState:        gitLabIssueTrackerState(payload),
	}
}

// gitLabIssueTrackerState derives the provider-neutral tracker state for a GitLab
// issue. A scoped `workflow::*` label wins (richer than open/closed); otherwise
// the issue's open/closed state is used (closed → completed, open → started).
// State vocabulary matches the Linear tracker path (started|completed) so the
// reconciliation conflict-warning logic stays provider-agnostic.
func gitLabIssueTrackerState(payload gitLabWebhookPayload) string {
	for _, label := range payload.Labels {
		title := strings.ToLower(strings.TrimSpace(label.Title))
		const prefix = "workflow::"
		if !strings.HasPrefix(title, prefix) {
			continue
		}
		switch strings.TrimSpace(strings.TrimPrefix(title, prefix)) {
		case "done", "completed", "complete", "closed":
			return "completed"
		case "doing", "in progress", "in-progress", "started":
			return "started"
		}
	}
	if strings.EqualFold(strings.TrimSpace(payload.ObjectAttributes.State), "closed") {
		return "completed"
	}
	return "started"
}

func gitLabMRDeliveryState(action string, state string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	state = strings.ToLower(strings.TrimSpace(state))
	if action == "merge" || state == "merged" {
		return coretypes.DeliveryStateMerged
	}
	if action == "close" || state == "closed" {
		return coretypes.DeliveryStateClosed
	}
	return coretypes.DeliveryStateOpened
}

type gitLabWebhookPayload struct {
	ObjectKind string `json:"object_kind"`
	EventType  string `json:"event_type"`
	Project    struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		WebURL            string `json:"web_url"`
	} `json:"project"`
	ObjectAttributes struct {
		ID              int    `json:"id"`
		IID             int    `json:"iid"`
		Action          string `json:"action"`
		State           string `json:"state"`
		Title           string `json:"title"`
		Description     string `json:"description"`
		Note            string `json:"note"`
		NoteableType    string `json:"noteable_type"`
		URL             string `json:"url"`
		SourceBranch    string `json:"source_branch"`
		TargetBranch    string `json:"target_branch"`
		MergeCommitSHA  string `json:"merge_commit_sha"`
		TargetProjectID int    `json:"target_project_id"`
	} `json:"object_attributes"`
	MergeRequest struct {
		Title string `json:"title"`
	} `json:"merge_request"`
	Issue struct {
		Title string `json:"title"`
	} `json:"issue"`
	User struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	// Labels are populated on Issue Hook payloads; used to read a scoped
	// workflow:: label for richer tracker status than open/closed.
	Labels []struct {
		Title string `json:"title"`
	} `json:"labels"`
}

func parseGitLabWebhookPayload(raw string) (gitLabWebhookPayload, error) {
	var payload gitLabWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("%w: invalid gitlab webhook payload", coretypes.ErrValidation)
	}
	payload.ObjectKind = strings.ToLower(strings.TrimSpace(payload.ObjectKind))
	payload.EventType = strings.ToLower(strings.TrimSpace(payload.EventType))
	payload.Project.PathWithNamespace = strings.TrimSpace(payload.Project.PathWithNamespace)
	if payload.Project.ID == 0 && payload.ObjectAttributes.TargetProjectID != 0 {
		payload.Project.ID = payload.ObjectAttributes.TargetProjectID
	}
	if payload.Project.ID == 0 && payload.Project.PathWithNamespace == "" {
		return payload, fmt.Errorf("%w: gitlab project id or path is required", coretypes.ErrValidation)
	}
	return payload, nil
}
