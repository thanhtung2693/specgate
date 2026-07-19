package integrations

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
	"github.com/specgate/doc-registry/internal/workboard"
)

type LinearHandoffInput struct {
	ChangeRequestID string
	IntegrationID   string
	ResourceID      string
}

type LinearHandoffResult struct {
	Link    TrackerLink
	Created bool
}

// HandoffLinear creates exactly one Linear issue for a Ready work item. The
// stable caller-selected issue ID lets a retry recover an issue created before
// a lost response or a local persistence failure.
func (s *Service) HandoffLinear(ctx context.Context, in LinearHandoffInput) (*LinearHandoffResult, error) {
	in.ChangeRequestID = strings.TrimSpace(in.ChangeRequestID)
	in.IntegrationID = strings.TrimSpace(in.IntegrationID)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	if in.ChangeRequestID == "" || in.IntegrationID == "" || in.ResourceID == "" {
		return nil, fmt.Errorf("%w: change_request_id, integration_id, and resource_id are required", ErrValidation)
	}
	if s.workBoard == nil {
		return nil, fmt.Errorf("%w: workboard store is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, in.IntegrationID)
	if err != nil {
		return nil, err
	}
	if integration.Provider != ProviderLinear || integration.Status != StatusConnected {
		return nil, fmt.Errorf("%w: integration must be a connected Linear integration", ErrValidation)
	}
	ctx, err = bindIntegrationWorkspace(ctx, integration)
	if err != nil {
		return nil, err
	}
	resource, err := s.resources.GetResource(ctx, in.IntegrationID, in.ResourceID)
	if err != nil {
		return nil, err
	}
	if resource.IntegrationID != in.IntegrationID || resource.ResourceType != ResourceTypeTeam || strings.TrimSpace(resource.ExternalID) == "" {
		return nil, fmt.Errorf("%w: resource must be a Linear team belonging to the integration", ErrValidation)
	}
	changeRequest, err := s.workBoard.GetChangeRequest(ctx, in.ChangeRequestID)
	if err != nil {
		return nil, err
	}
	phase := changeRequest.Phase
	if phase == "" {
		phase = changeRequest.DerivePhase()
	}
	if phase != workboard.BoardPhaseReady || changeRequest.Archived {
		return nil, fmt.Errorf("%w: only Ready work items can be handed off", ErrValidation)
	}
	criteria, err := s.workBoard.ListAcceptanceCriteria(ctx, changeRequest.ID)
	if err != nil {
		return nil, err
	}

	var result *LinearHandoffResult
	handoff := func(store TrackerLinkStore) error {
		links, err := store.ListTrackerLinksByChangeRequest(ctx, changeRequest.ID)
		if err != nil {
			return err
		}
		if len(links) > 0 {
			result = &LinearHandoffResult{Link: links[0], Created: false}
			return nil
		}

		issueID := deterministicLinearIssueID(integration.WorkspaceID, changeRequest.ID)
		token, err := s.ResolveAPIToken(ctx, integration.ID)
		if err != nil {
			return err
		}
		bearer := strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth
		issue, err := linearprovider.GetIssue(ctx, token, bearer, issueID)
		if err != nil {
			return err
		}
		if issue == nil {
			createdIssue, createErr := linearprovider.CreateIssue(ctx, token, bearer, linearprovider.IssueInput{
				ID: issueID, TeamID: resource.ExternalID, Title: changeRequest.Title,
				Description: renderLinearIssueDescription(*changeRequest, criteria),
			})
			if createErr == nil {
				issue = &createdIssue
			} else if linearprovider.IsAmbiguousCreateError(createErr) {
				issue, err = linearprovider.GetIssue(ctx, token, bearer, issueID)
				if err != nil || issue == nil {
					if err != nil {
						return err
					}
					return fmt.Errorf("%w: Linear issue create failed without recovery", ErrUpstream)
				}
			} else {
				return createErr
			}
		}
		if strings.TrimSpace(issue.ID) == "" || strings.TrimSpace(issue.Identifier) == "" {
			return fmt.Errorf("%w: Linear returned an incomplete issue", ErrUpstream)
		}
		if strings.TrimSpace(issue.TeamID) != strings.TrimSpace(resource.ExternalID) {
			return fmt.Errorf("%w: Linear issue does not belong to the selected team", ErrValidation)
		}
		link, err := store.UpsertTrackerLink(ctx, TrackerLink{
			IntegrationID: integration.ID, ResourceID: resource.ID, FeatureID: changeRequest.FeatureID,
			ChangeRequestID: changeRequest.ID, ExternalID: issue.ID, ExternalKey: issue.Identifier,
			URL: issue.URL, Title: changeRequest.Title, State: TrackerStateOpened,
		})
		if err != nil {
			return err
		}
		result = &LinearHandoffResult{Link: *link, Created: true}
		return nil
	}
	if s.handoffLocker != nil {
		err = s.handoffLocker.WithChangeRequestHandoffLock(ctx, changeRequest.ID, handoff)
	} else {
		err = handoff(s.trackerLinks)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func deterministicLinearIssueID(workspaceID, changeRequestID string) string {
	sum := sha256.Sum256([]byte(workspaceID + "\x00" + changeRequestID))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func renderLinearIssueDescription(changeRequest workboard.ChangeRequest, criteria []workboard.AcceptanceCriterion) string {
	var b strings.Builder
	b.WriteString("## Intent\n")
	b.WriteString(strings.TrimSpace(changeRequest.IntentMD))
	b.WriteString("\n\n## Acceptance criteria\n")
	for _, criterion := range criteria {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(criterion.Text))
		b.WriteByte('\n')
	}
	b.WriteString("\n## Pick up approved context\n")
	fmt.Fprintf(&b, "`specgate work context %s --json`\n", changeRequest.Key)
	b.WriteString("\n## Delivery marker\nInclude this exact marker in the pull or merge request:\n")
	fmt.Fprintf(&b, "`<!-- specgate-work-ref: %s -->`\n", changeRequest.Key)
	return b.String()
}
