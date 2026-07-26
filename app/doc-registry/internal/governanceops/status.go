package governanceops

import (
	"context"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// humanActionGates are gates whose pending state requires a human to act in the
// web UI — the agent cannot perform these actions.
var humanActionGates = map[string]string{
	"canonical_spec": "Promote spec to canonical",
}

type resolveMatch struct {
	cr   workboard.ChangeRequest
	link integrations.TrackerLink
}

// ResolveWorkRef resolves a flexible work reference to its SpecGate change
// request. Resolution order:
//  1. Direct change-request ID via GetChangeRequest.
//  2. Case-insensitive Key scan across CRs, including archived CRs for explicit lookup.
//  3. Full HTTPS URL → exact tracker link URL, optionally narrowed by Provider.
//  4. Bare tracker key → explicit Provider required.
func (s *Service) ResolveWorkRef(ctx context.Context, in ResolveWorkRefInput) (ResolvedWork, error) {
	if s.WorkBoard == nil {
		return ResolvedWork{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	ref := strings.TrimSpace(in.Ref)
	provider := strings.ToLower(strings.TrimSpace(in.Provider))

	// 1. Direct ID lookup.
	if cr, err := s.WorkBoard.GetChangeRequest(ctx, ref); err == nil {
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return ResolvedWork{}, ErrNotFound
		}
		return crToResolved(cr, integrations.TrackerLink{}), nil
	}

	// 2. Case-insensitive Key scan (also loads CRs for later explicit lookup
	// paths). Archive hides items from queues, but a known ref should remain
	// inspectable.
	crs, err := s.WorkBoard.ListChangeRequests(ctx, true)
	if err != nil {
		return ResolvedWork{}, err
	}
	if selected := trustedWorkspace(ctx); selected != "" {
		scoped := crs[:0]
		for i := range crs {
			if strings.TrimSpace(crs[i].WorkspaceID) == selected {
				scoped = append(scoped, crs[i])
			}
		}
		crs = scoped
	}
	refUpper := strings.ToUpper(ref)
	for i := range crs {
		if err := requireChangeRequestWorkspace(ctx, &crs[i]); err != nil {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(crs[i].Key)) == refUpper {
			return crToResolved(&crs[i], integrations.TrackerLink{}), nil
		}
	}
	// 3. Full HTTPS URL → exact tracker link URL.
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return s.resolveByURL(ctx, crs, ref, provider)
	}

	// 4. Bare tracker key requires Provider.
	if provider == "" {
		return ResolvedWork{}, ErrNotFound
	}
	return s.resolveByTrackerKey(ctx, crs, ref, provider)
}

func (s *Service) resolveByURL(ctx context.Context, crs []workboard.ChangeRequest, rawURL, providerHint string) (ResolvedWork, error) {
	if s.Trackers == nil {
		return ResolvedWork{}, fmt.Errorf("%w: tracker links not configured", ErrUnavailable)
	}
	allowed := map[string]struct{}{}
	if providerHint != "" {
		allIntegrations, err := s.Trackers.List(ctx)
		if err != nil {
			return ResolvedWork{}, err
		}
		for _, intg := range allIntegrations {
			if strings.EqualFold(intg.Provider, providerHint) {
				allowed[intg.ID] = struct{}{}
			}
		}
		if len(allowed) == 0 {
			return ResolvedWork{}, fmt.Errorf("no integrations configured for provider %q", providerHint)
		}
	}

	normalURL := normalizeURL(rawURL)
	var best *resolveMatch
	for i := range crs {
		links, err := s.Trackers.ListTrackerLinks(ctx, crs[i].ID)
		if err != nil {
			return ResolvedWork{}, err
		}
		for j := range links {
			link := links[j]
			if len(allowed) > 0 {
				if _, ok := allowed[link.IntegrationID]; !ok {
					continue
				}
			}
			if normalizeURL(link.URL) != normalURL {
				continue
			}
			candidate := &resolveMatch{cr: crs[i], link: link}
			if best == nil || link.UpdatedAt.After(best.link.UpdatedAt) {
				best = candidate
			}
		}
	}
	if best == nil {
		return ResolvedWork{}, ErrNotFound
	}
	return crToResolved(&best.cr, best.link), nil
}

func (s *Service) resolveByTrackerKey(ctx context.Context, crs []workboard.ChangeRequest, key, provider string) (ResolvedWork, error) {
	if s.Trackers == nil {
		return ResolvedWork{}, fmt.Errorf("%w: tracker links not configured", ErrUnavailable)
	}
	allIntegrations, err := s.Trackers.List(ctx)
	if err != nil {
		return ResolvedWork{}, err
	}

	allowed := map[string]struct{}{}
	for _, intg := range allIntegrations {
		if strings.EqualFold(intg.Provider, provider) {
			allowed[intg.ID] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return ResolvedWork{}, fmt.Errorf("no integrations configured for provider %q", provider)
	}

	var best *resolveMatch
	for i := range crs {
		links, err := s.Trackers.ListTrackerLinks(ctx, crs[i].ID)
		if err != nil {
			return ResolvedWork{}, err
		}
		for j := range links {
			link := links[j]
			if _, ok := allowed[link.IntegrationID]; !ok {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(link.ExternalKey), key) {
				continue
			}
			candidate := &resolveMatch{cr: crs[i], link: link}
			if best == nil || link.UpdatedAt.After(best.link.UpdatedAt) {
				best = candidate
			}
		}
	}
	if best == nil {
		return ResolvedWork{}, ErrNotFound
	}
	return crToResolved(&best.cr, best.link), nil
}

// GovernanceStatus returns a phase-count aggregate snapshot of active work
// items plus stale-warning attention list. WorkspaceID narrows the snapshot to
// locally attributed work items in the selected workspace.
func (s *Service) GovernanceStatus(ctx context.Context, in GovernanceStatusInput) (GovernanceStatusResult, error) {
	if s.WorkBoard == nil {
		return GovernanceStatusResult{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	crs, err := s.WorkBoard.ListChangeRequests(ctx, false)
	if err != nil {
		return GovernanceStatusResult{}, err
	}
	warnings, err := s.WorkBoard.ListStaleWarnings(ctx, workboard.StaleWarningFilter{})
	if err != nil {
		return GovernanceStatusResult{}, err
	}

	warnByCR := make(map[string][]string, len(warnings))
	for _, w := range warnings {
		if w.ChangeRequestID == "" {
			continue
		}
		warnByCR[w.ChangeRequestID] = append(warnByCR[w.ChangeRequestID], string(w.Code))
	}

	result := GovernanceStatusResult{
		Attention: make([]GovernanceStatusAttentionItem, 0),
	}
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	if workspaceID == "" {
		workspaceID = trustedWorkspace(ctx)
	}
	for i := range crs {
		cr := &crs[i]
		if workspaceID != "" && strings.TrimSpace(cr.WorkspaceID) != workspaceID {
			continue
		}
		phase := cr.Phase
		if phase == "" {
			phase = workboard.BoardPhase(cr.DerivePhase())
		}
		switch phase {
		case workboard.BoardPhaseIntake:
			result.Counts.Intake++
		case workboard.BoardPhaseReview:
			result.Counts.Review++
		case workboard.BoardPhaseReady:
			result.Counts.Ready++
		case workboard.BoardPhaseDelivered:
			result.Counts.Delivered++
		default:
			result.Counts.Intake++
		}
		result.Counts.Total++

		// Delivered items have explicit human acceptance — nothing left to act
		// on, so they never surface in attention even with stale warnings.
		if phase == workboard.BoardPhaseDelivered {
			continue
		}
		if issues, ok := warnByCR[cr.ID]; ok {
			result.Attention = append(result.Attention, GovernanceStatusAttentionItem{
				ChangeRequestID: cr.ID,
				Key:             cr.Key,
				Title:           cr.Title,
				Phase:           string(phase),
				Issues:          issues,
			})
		}
	}
	result.Summary = buildSummary(result.Counts, len(result.Attention))
	return result, nil
}

// WorkStatus returns a compact gate + AC + delivery snapshot for one CR.
func (s *Service) WorkStatus(ctx context.Context, in ResolveWorkRefInput) (WorkStatusResult, error) {
	if s.WorkBoard == nil {
		return WorkStatusResult{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(in.Ref)
	if id == "" {
		return WorkStatusResult{}, fmt.Errorf("ref is required")
	}

	cr, err := s.WorkBoard.GetChangeRequest(ctx, id)
	if err != nil {
		return WorkStatusResult{}, err
	}
	if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
		return WorkStatusResult{}, err
	}
	phase := cr.Phase
	if phase == "" {
		phase = cr.DerivePhase()
	}

	acs, err := s.WorkBoard.ListAcceptanceCriteria(ctx, id)
	if err != nil {
		return WorkStatusResult{}, err
	}
	acsDone := 0
	for _, ac := range acs {
		if ac.Done {
			acsDone++
		}
	}

	runs, err := s.WorkBoard.ListGateRuns(ctx, id, 200)
	if err != nil {
		return WorkStatusResult{}, err
	}

	latestPerGate := map[string]workboard.GateRun{}
	for _, run := range runs {
		existing, ok := latestPerGate[run.Gate]
		if !ok || run.CreatedAt.After(existing.CreatedAt) {
			latestPerGate[run.Gate] = run
		}
	}

	canonicalOrder := []string{
		"scope_clear", "success_metric_measurable", "rollback_defined",
		"ac_coverage", "execution_brief", "canonical_spec",
	}
	seen := map[string]bool{}
	gateList := make([]GateSummary, 0, len(latestPerGate))
	for _, gate := range canonicalOrder {
		if run, ok := latestPerGate[gate]; ok {
			gateList = append(gateList, GateSummary{Gate: run.Gate, State: string(run.State), Hint: run.Hint})
			seen[gate] = true
		}
	}
	for gate, run := range latestPerGate {
		if !seen[gate] && gate != "delivery_review" {
			gateList = append(gateList, GateSummary{Gate: run.Gate, State: string(run.State), Hint: run.Hint})
		}
	}

	var deliveryReview *DeliveryReviewSummary
	latestDelivery, err := authoritativeDeliveryReviewRun(ctx, s.WorkBoard, id)
	if err != nil {
		return WorkStatusResult{}, err
	}
	if latest := latestDelivery; latest != nil {
		deliveryReview = &DeliveryReviewSummary{
			Verdict:    string(latest.State),
			Hint:       latest.Hint,
			ReviewedAt: formatRFC3339(latest.CreatedAt),
			Executor:   latest.Executor,
		}
		deliveryReview.Actor, deliveryReview.Note, deliveryReview.Summary = deliveryRunAuditFields(*latest)
	}

	base := strings.TrimRight(s.AppBaseURL, "/")
	pendingActions := make([]PendingHumanAction, 0)
	for _, gate := range canonicalOrder {
		label, isHuman := humanActionGates[gate]
		if !isHuman {
			continue
		}
		run, ok := latestPerGate[gate]
		if !ok || run.State != workboard.NextActionStatePending {
			continue
		}
		action := PendingHumanAction{Action: gate, Label: label}
		if base != "" {
			action.URL = base + "/work-items/" + id
		}
		pendingActions = append(pendingActions, action)
	}

	return WorkStatusResult{
		ChangeRequestID:     id,
		Title:               cr.Title,
		Phase:               string(phase),
		WorkType:            string(cr.WorkType),
		Gates:               gateList,
		ACsDone:             acsDone,
		ACsTotal:            len(acs),
		DeliveryReview:      deliveryReview,
		PendingHumanActions: pendingActions,
	}, nil
}

// GateHistory returns gate run history for a CR, optionally filtered to one gate.
func (s *Service) GateHistory(ctx context.Context, in GateHistoryInput) (GateHistoryResult, error) {
	if s.WorkBoard == nil {
		return GateHistoryResult{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(in.ChangeRequestID)
	if id == "" {
		return GateHistoryResult{}, fmt.Errorf("change_request_id is required")
	}
	if trustedWorkspace(ctx) != "" {
		cr, err := s.WorkBoard.GetChangeRequest(ctx, id)
		if err != nil {
			return GateHistoryResult{}, err
		}
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return GateHistoryResult{}, err
		}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	gateFilter := strings.TrimSpace(in.Gate)

	runs, err := s.WorkBoard.ListGateRuns(ctx, id, limit*2)
	if err != nil {
		return GateHistoryResult{}, err
	}

	history := make([]GateRunEntry, 0, len(runs))
	for _, run := range runs {
		if gateFilter != "" && run.Gate != gateFilter {
			continue
		}
		history = append(history, GateRunEntry{
			GateRunID: run.ID,
			Gate:      run.Gate,
			State:     string(run.State),
			Hint:      run.Hint,
			CreatedAt: formatRFC3339(run.CreatedAt),
		})
		if len(history) >= limit {
			break
		}
	}
	return GateHistoryResult{ChangeRequestID: id, Runs: history}, nil
}

// DeliveryStatus returns the authoritative delivery-review verdict for a CR.
