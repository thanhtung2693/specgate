// Package gitlab holds Service-independent GitLab webhook normalization.
package gitlab

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

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
		HeadSHA:         payload.ObjectAttributes.LastCommit.ID,
		MergeCommitSHA:  payload.ObjectAttributes.MergeCommitSHA,
		DeliveryState:   gitLabMRDeliveryState(payload.ObjectAttributes.Action, payload.ObjectAttributes.State),
	}
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
		ID             int    `json:"id"`
		IID            int    `json:"iid"`
		Action         string `json:"action"`
		State          string `json:"state"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		Note           string `json:"note"`
		NoteableType   string `json:"noteable_type"`
		URL            string `json:"url"`
		SourceBranch   string `json:"source_branch"`
		TargetBranch   string `json:"target_branch"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		LastCommit     struct {
			ID string `json:"id"`
		} `json:"last_commit"`
		TargetProjectID int `json:"target_project_id"`
	} `json:"object_attributes"`
	MergeRequest struct {
		Title string `json:"title"`
	} `json:"merge_request"`
	User struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
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
