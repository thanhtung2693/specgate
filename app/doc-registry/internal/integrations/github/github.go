// Package github holds Service-independent GitHub webhook normalization.
package github

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// ParseAndNormalize parses a raw GitHub pull_request webhook body and maps it
// onto the provider-neutral coretypes.NormalizedDelivery. externalEventID is
// the dedup key (X-GitHub-Delivery or a body hash) supplied by the caller. The
// parent reads repo identity off the returned ProjectID / ProjectKey, so the
// raw payload struct never crosses the package boundary.
func ParseAndNormalize(raw, externalEventID string) (coretypes.NormalizedDelivery, error) {
	payload, err := parseGitHubWebhookPayload(raw)
	if err != nil {
		return coretypes.NormalizedDelivery{}, err
	}
	return gitHubNormalizedDelivery(payload, externalEventID, raw), nil
}

// ParseCommentScopeDrift parses a GitHub issue_comment webhook body into the
// provider-neutral comment shape used for scope-drift feedback ingestion.
func ParseCommentScopeDrift(raw, externalEventID string) (coretypes.NormalizedComment, error) {
	payload, err := parseGitHubCommentPayload(raw)
	if err != nil {
		return coretypes.NormalizedComment{}, err
	}
	return coretypes.NormalizedComment{
		Provider:        coretypes.ProviderGitHub,
		EventType:       coretypes.WebhookEventComment,
		ExternalEventID: externalEventID,
		RawPayload:      raw,
		ProjectID:       payload.Repository.ID,
		ProjectKey:      payload.Repository.FullName,
		ExternalID:      strconv.Itoa(payload.Comment.ID),
		ExternalKey:     payload.Repository.FullName + "#comment-" + strconv.Itoa(payload.Comment.ID),
		Title:           payload.Issue.Title,
		Body:            payload.Comment.Body,
		URL:             strings.TrimSpace(payload.Comment.HTMLURL),
		Author:          strings.TrimSpace(payload.Comment.User.Login),
	}, nil
}

func gitHubNormalizedDelivery(payload gitHubWebhookPayload, externalEventID, raw string) coretypes.NormalizedDelivery {
	state := gitHubPRDeliveryState(payload.Action, payload.PullRequest.Merged)
	return coretypes.NormalizedDelivery{
		Provider:        coretypes.ProviderGitHub,
		EventType:       coretypes.WebhookEventMergeRequest,
		ExternalEventID: externalEventID,
		RawPayload:      raw,
		ProjectID:       payload.Repository.ID,
		ProjectKey:      payload.Repository.FullName,
		ExternalID:      strconv.Itoa(payload.PullRequest.ID),
		IID:             payload.PullRequest.Number,
		ExternalKey:     payload.Repository.FullName + "#" + strconv.Itoa(payload.PullRequest.Number),
		Title:           payload.PullRequest.Title,
		Description:     payload.PullRequest.Body,
		URL:             payload.PullRequest.HTMLURL,
		Action:          payload.Action,
		RawState:        state,
		SourceBranch:    payload.PullRequest.Head.Ref,
		TargetBranch:    payload.PullRequest.Base.Ref,
		HeadSHA:         payload.PullRequest.Head.SHA,
		MergeCommitSHA:  payload.PullRequest.MergeCommitSHA,
		DeliveryState:   state,
	}
}

func gitHubPRDeliveryState(action string, merged bool) string {
	if strings.ToLower(strings.TrimSpace(action)) == "closed" {
		if merged {
			return coretypes.DeliveryStateMerged
		}
		return coretypes.DeliveryStateClosed
	}
	return coretypes.DeliveryStateOpened
}

type gitHubWebhookPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		ID             int    `json:"id"`
		Number         int    `json:"number"`
		Title          string `json:"title"`
		Body           string `json:"body"`
		HTMLURL        string `json:"html_url"`
		State          string `json:"state"`
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		Head           struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
	Repository struct {
		ID       int    `json:"id"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type gitHubCommentPayload struct {
	Action  string `json:"action"`
	Comment struct {
		ID      int    `json:"id"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Issue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"issue"`
	Repository struct {
		ID       int    `json:"id"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func parseGitHubWebhookPayload(raw string) (gitHubWebhookPayload, error) {
	var payload gitHubWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("%w: invalid github webhook payload", coretypes.ErrValidation)
	}
	payload.Action = strings.ToLower(strings.TrimSpace(payload.Action))
	payload.Repository.FullName = strings.TrimSpace(payload.Repository.FullName)
	if payload.Repository.ID == 0 && payload.Repository.FullName == "" {
		return payload, fmt.Errorf("%w: github repository id or full_name is required", coretypes.ErrValidation)
	}
	return payload, nil
}

func parseGitHubCommentPayload(raw string) (gitHubCommentPayload, error) {
	var payload gitHubCommentPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("%w: invalid github comment payload", coretypes.ErrValidation)
	}
	payload.Repository.FullName = strings.TrimSpace(payload.Repository.FullName)
	payload.Comment.Body = strings.TrimSpace(payload.Comment.Body)
	if payload.Repository.ID == 0 && payload.Repository.FullName == "" {
		return payload, fmt.Errorf("%w: github repository id or full_name is required", coretypes.ErrValidation)
	}
	if payload.Comment.ID == 0 {
		return payload, fmt.Errorf("%w: github comment id is required", coretypes.ErrValidation)
	}
	return payload, nil
}
