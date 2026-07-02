// Package github holds the Service-independent GitHub provider code: the
// outbound tracker adapter, webhook parse+normalize, and the derived MCP-entry
// builder. It depends only on coretypes (leaf domain types/registry) and
// internal/githubapi — never the parent integrations package, so there is no
// import cycle. The parent blank-imports this package to trigger adapter
// registration and delegates its two Service methods (HandleGitHubWebhook,
// ListGitHubMCPServers) to the pure functions here.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/githubapi"
	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func init() {
	coretypes.RegisterTracker(coretypes.ProviderGitHub, githubTracker{})
}

// githubTracker resolves the repo (owner/repo, from a resource_type=project
// resource's external_key or config_json.repo — required, no auto-resolve) and
// creates the issue via the GitHub REST API. apiURL is derived from the
// integration's base_url host.
type githubTracker struct{}

func (githubTracker) CreateIssue(ctx context.Context, h coretypes.Handoff) (*coretypes.TrackerIssue, error) {
	repo := explicitGitHubRepo(h.Integration, h.Resources)
	if repo == "" {
		return nil, fmt.Errorf("%w: github integration has no repo configured (add a resource_type=project resource with external_key=owner/repo or config_json.repo)", coretypes.ErrValidation)
	}
	apiURL := gitHubAPIURL(h.Integration.BaseURL)
	client := githubapi.NewClient(githubapi.ClientConfig{APIURL: apiURL, Token: h.APIToken, Repo: repo})
	issue, err := client.CreateIssue(ctx, githubapi.CreateIssueRequest{
		Title: h.Title,
		Body:  h.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", coretypes.ErrUpstream, err)
	}
	return &coretypes.TrackerIssue{Identifier: fmt.Sprintf("#%d", issue.Number), URL: issue.URL}, nil
}

// explicitGitHubRepo returns the owner/repo the issue is filed under: a
// resource_type=project resource (external_key path, the GitHub slug — GitHub
// has no numeric path id like GitLab) or config_json.repo, or "" when none is
// configured.
func explicitGitHubRepo(integration *coretypes.Integration, resources []coretypes.Resource) string {
	for _, r := range resources {
		if r.ResourceType == coretypes.ResourceTypeProject {
			if key := strings.TrimSpace(r.ExternalKey); key != "" {
				return key
			}
		}
	}
	return strings.TrimSpace(coretypes.IntegrationConfigString(integration, "repo"))
}

// gitHubAPIURL derives the REST API root from the integration's base_url. For
// github.com (or an empty base_url) it is the hosted API https://api.github.com;
// for a GitHub Enterprise host https://ghe.example it is the GHE convention
// https://ghe.example/api/v3.
func gitHubAPIURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "https://api.github.com"
	}
	lower := strings.ToLower(base)
	if lower == "https://github.com" || lower == "http://github.com" ||
		strings.HasSuffix(lower, "://www.github.com") {
		return "https://api.github.com"
	}
	return base + "/api/v3"
}

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

// ParseWorkflowRun parses a raw GitHub workflow_run webhook body and maps it
// onto a provider-neutral NormalizedDelivery with DeliveryState=ci_passed.
// Returns ErrValidation when the conclusion is not "success" so the caller can
// ignore non-success runs without treating them as parse errors. per spec §11.
func ParseWorkflowRun(raw, externalEventID string) (coretypes.NormalizedDelivery, error) {
	payload, err := parseGitHubWorkflowRunPayload(raw)
	if err != nil {
		return coretypes.NormalizedDelivery{}, err
	}
	if payload.WorkflowRun.Conclusion != "success" {
		return coretypes.NormalizedDelivery{}, fmt.Errorf("%w: workflow_run conclusion %q is not success", coretypes.ErrValidation, payload.WorkflowRun.Conclusion)
	}
	return coretypes.NormalizedDelivery{
		Provider:        coretypes.ProviderGitHub,
		EventType:       coretypes.WebhookEventWorkflowRun,
		ExternalEventID: externalEventID,
		RawPayload:      raw,
		ProjectID:       payload.Repository.ID,
		ProjectKey:      payload.Repository.FullName,
		ExternalID:      strconv.FormatInt(payload.WorkflowRun.ID, 10),
		DeliveryState:   coretypes.DeliveryStateCIPassed,
		SourceBranch:    payload.WorkflowRun.HeadBranch,
		MergeCommitSHA:  payload.WorkflowRun.HeadSHA,
		URL:             payload.WorkflowRun.HTMLURL,
	}, nil
}

type gitHubWorkflowRunPayload struct {
	Action      string `json:"action"`
	WorkflowRun struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
		HeadBranch string `json:"head_branch"`
		HeadSHA    string `json:"head_sha"`
		HTMLURL    string `json:"html_url"`
	} `json:"workflow_run"`
	Repository struct {
		ID       int    `json:"id"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func parseGitHubWorkflowRunPayload(raw string) (gitHubWorkflowRunPayload, error) {
	var payload gitHubWorkflowRunPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("%w: invalid github workflow_run payload", coretypes.ErrValidation)
	}
	payload.WorkflowRun.Conclusion = strings.ToLower(strings.TrimSpace(payload.WorkflowRun.Conclusion))
	payload.WorkflowRun.HeadBranch = strings.TrimSpace(payload.WorkflowRun.HeadBranch)
	payload.Repository.FullName = strings.TrimSpace(payload.Repository.FullName)
	if payload.Repository.ID == 0 && payload.Repository.FullName == "" {
		return payload, fmt.Errorf("%w: github repository id or full_name is required", coretypes.ErrValidation)
	}
	return payload, nil
}
