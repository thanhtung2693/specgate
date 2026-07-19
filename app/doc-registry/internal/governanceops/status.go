package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

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
		case workboard.BoardPhaseDraft:
			result.Counts.Draft++
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
func (s *Service) DeliveryStatus(ctx context.Context, in DeliveryStatusInput) (DeliveryStatusResult, error) {
	if s.WorkBoard == nil {
		return DeliveryStatusResult{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(in.ChangeRequestID)
	if id == "" {
		return DeliveryStatusResult{}, fmt.Errorf("change_request_id is required")
	}
	if trustedWorkspace(ctx) != "" {
		cr, err := s.WorkBoard.GetChangeRequest(ctx, id)
		if err != nil {
			return DeliveryStatusResult{}, err
		}
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return DeliveryStatusResult{}, err
		}
	}

	latest, err := authoritativeDeliveryReviewRun(ctx, s.WorkBoard, id)
	if err != nil {
		return DeliveryStatusResult{}, err
	}
	completion, err := s.latestCompletionRecord(ctx, id)
	if err != nil {
		return DeliveryStatusResult{}, err
	}
	if latest == nil && completion == nil {
		return DeliveryStatusResult{ChangeRequestID: id, Found: false}, nil
	}
	wrapper, detail := decodeDeliveryReview("")
	if latest != nil {
		wrapper, detail = decodeDeliveryReview(latest.EvidenceJSON)
	}
	if completion != nil && (latest == nil ||
		strings.TrimSpace(wrapper.CompletionFeedbackEventID) != completion.Event.ID) {
		result := DeliveryStatusResult{
			ChangeRequestID: id,
			Found:           true,
			Verdict:         string(workboard.NextActionStateNeedsHumanReview),
			ReasonCode:      "delivery_review_outdated",
			Hint:            "The latest completion has not been reviewed; rerun delivery review before a human decision.",
		}
		if in.Detail {
			result.GitReceipt = completion.Payload.GitReceipt
			if peer, peerErr := s.peerReviewState(ctx, id); peerErr != nil {
				return DeliveryStatusResult{}, peerErr
			} else {
				result.PeerReview = peer
			}
		}
		return result, nil
	}
	result := DeliveryStatusResult{
		ChangeRequestID:  id,
		GateRunID:        latest.ID,
		Found:            true,
		Verdict:          string(latest.State),
		EvidenceVerdict:  wrapper.evidenceVerdict(*latest),
		ReasonCode:       detail.ReasonCode,
		Hint:             latest.Hint,
		Confidence:       wrapper.reviewConfidence(*latest),
		JudgeModel:       wrapper.judgeModel(),
		EvalSuite:        wrapper.evalSuiteVersion(),
		ReviewedAt:       formatRFC3339(latest.CreatedAt),
		Executor:         latest.Executor,
		Actor:            wrapper.actor(),
		Note:             wrapper.Note,
		Summary:          workboard.DeliveryDecisionSummary(*latest, wrapper.actor(), wrapper.Note),
		OutstandingMD:    deliveryReviewOutstandingMD(*latest, detail),
		AssuranceSources: deliveryReviewAssuranceSources(detail),
	}
	if in.Detail {
		if completion != nil {
			result.GitReceipt = completion.Payload.GitReceipt
		}
		if peer, err := s.peerReviewState(ctx, id); err != nil {
			return DeliveryStatusResult{}, err
		} else {
			result.PeerReview = peer
		}
		for _, c := range detail.Criteria {
			result.PerCriterion = append(result.PerCriterion, CriterionReview{
				CriterionID:         c.CriterionID,
				Text:                c.Text,
				Verdict:             c.Verdict,
				Why:                 c.Why,
				VerificationBinding: c.VerificationBinding,
				TrustTier:           c.TrustTier,
			})
		}
		for _, c := range detail.Checks {
			result.Checks = append(result.Checks, CheckResult{Name: c.Name, Status: c.Status, Detail: c.Detail})
		}
	}
	return result, nil
}

func (s *Service) peerReviewState(ctx context.Context, changeRequestID string) (PeerReviewState, error) {
	if s.FeedbackStore == nil {
		return PeerReviewState{State: "not_run"}, nil
	}
	completion, err := s.latestCompletionRecord(ctx, changeRequestID)
	if err != nil {
		return PeerReviewState{}, err
	}
	rows, err := s.FeedbackStore.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
		ChangeRequestID: changeRequestID,
		EventType:       integrations.FeedbackEventCodingAgentPeerReviewed,
		Limit:           200,
	})
	if err != nil {
		return PeerReviewState{}, err
	}
	var peer *integrations.GovernanceFeedbackEvent
	for i := range rows {
		row := &rows[i]
		if row.EventType == integrations.FeedbackEventCodingAgentPeerReviewed &&
			(peer == nil || governanceFeedbackEventNewer(*row, *peer)) {
			peer = row
		}
	}
	if peer == nil {
		return PeerReviewState{State: "not_run"}, nil
	}
	state := PeerReviewState{State: "failed", ReviewedAt: formatRFC3339(peer.CreatedAt)}
	var review ReportFeedbackInput
	if err := json.Unmarshal([]byte(peer.PayloadJSON), &review); err != nil {
		return state, nil
	}
	state.AgentName = strings.TrimSpace(review.Agent.Name)
	if completion == nil || review.PeerReviewOf == nil || review.PeerReviewOf.CompletionFeedbackEventID != completion.Event.ID {
		state.State = "stale"
		return state, nil
	}
	if completion.Payload.GitReceipt == nil ||
		review.PeerReviewOf.GitReceipt == nil ||
		!reflect.DeepEqual(*completion.Payload.GitReceipt, *review.PeerReviewOf.GitReceipt) {
		state.State = "stale"
		return state, nil
	}
	if len(review.Criteria) == 0 {
		return state, nil
	}
	for _, criterion := range review.Criteria {
		if strings.TrimSpace(strings.ToLower(criterion.Claim)) != "satisfied" {
			return state, nil
		}
	}
	state.State = "passed"
	return state, nil
}

// --- helpers ---

func crToResolved(cr *workboard.ChangeRequest, link integrations.TrackerLink) ResolvedWork {
	phase := cr.Phase
	if phase == "" {
		phase = cr.DerivePhase()
	}
	return ResolvedWork{
		ChangeRequestID:  cr.ID,
		ChangeRequestKey: cr.Key,
		FeatureID:        cr.FeatureID,
		Title:            cr.Title,
		Phase:            string(phase),
		IssueKey:         link.ExternalKey,
		IssueURL:         link.URL,
	}
}

func buildSummary(counts GovernanceStatusCounts, attention int) string {
	if counts.Total == 0 {
		return "No active work items."
	}
	parts := make([]string, 0, 6)
	if counts.Intake > 0 {
		parts = append(parts, fmt.Sprintf("%d in intake", counts.Intake))
	}
	if counts.Draft > 0 {
		parts = append(parts, fmt.Sprintf("%d in draft", counts.Draft))
	}
	if counts.Review > 0 {
		parts = append(parts, fmt.Sprintf("%d in review", counts.Review))
	}
	if counts.Ready > 0 {
		parts = append(parts, fmt.Sprintf("%d ready", counts.Ready))
	}
	if counts.Delivered > 0 {
		parts = append(parts, fmt.Sprintf("%d delivered", counts.Delivered))
	}
	noun := "work items"
	if counts.Total == 1 {
		noun = "work item"
	}
	summary := fmt.Sprintf("%d active %s", counts.Total, noun)
	if len(parts) > 0 {
		summary += " — " + strings.Join(parts, ", ")
	}
	if attention > 0 {
		attn := "items need"
		if attention == 1 {
			attn = "item needs"
		}
		summary += fmt.Sprintf(" — %d %s attention", attention, attn)
	}
	return summary
}

func latestDeliveryRun(runs []workboard.GateRun) *workboard.GateRun {
	var human, platform *workboard.GateRun
	for i := range runs {
		if runs[i].Gate != "delivery_review" {
			continue
		}
		if runs[i].Executor == workboard.GateRunExecutorHuman {
			if human == nil || gateRunNewer(runs[i], *human) {
				cp := runs[i]
				human = &cp
			}
		} else if platform == nil || gateRunNewer(runs[i], *platform) {
			cp := runs[i]
			platform = &cp
		}
	}
	if human == nil {
		return platform
	}
	if platform == nil {
		return human
	}
	var humanBinding, platformBinding struct {
		CompletionFeedbackEventID string `json:"completion_feedback_event_id"`
	}
	_ = json.Unmarshal([]byte(human.EvidenceJSON), &humanBinding)
	_ = json.Unmarshal([]byte(platform.EvidenceJSON), &platformBinding)
	if gateRunNewer(*platform, *human) &&
		strings.TrimSpace(humanBinding.CompletionFeedbackEventID) != "" &&
		strings.TrimSpace(platformBinding.CompletionFeedbackEventID) != "" &&
		humanBinding.CompletionFeedbackEventID != platformBinding.CompletionFeedbackEventID {
		return platform
	}
	return human
}

func gateRunNewer(candidate, current workboard.GateRun) bool {
	return candidate.CreatedAt.After(current.CreatedAt) ||
		(candidate.CreatedAt.Equal(current.CreatedAt) && candidate.ID > current.ID)
}

type authoritativeDeliveryReviewReader interface {
	AuthoritativeDeliveryReviewRun(
		context.Context,
		string,
	) (*workboard.GateRun, error)
}

func authoritativeDeliveryReviewRun(
	ctx context.Context,
	reader WorkBoardReader,
	changeRequestID string,
) (*workboard.GateRun, error) {
	if authoritative, ok := reader.(authoritativeDeliveryReviewReader); ok {
		return authoritative.AuthoritativeDeliveryReviewRun(ctx, changeRequestID)
	}
	runs, err := reader.ListGateRuns(ctx, changeRequestID, 500)
	if err != nil {
		return nil, err
	}
	return latestDeliveryRun(runs), nil
}

type deliveryReviewWrapper struct {
	EvidenceContractVersion   string   `json:"evidence_contract_version,omitempty"`
	Verdict                   string   `json:"verdict,omitempty"`
	EvidenceVerdict           string   `json:"evidence_verdict,omitempty"`
	EvidenceConfidence        *float64 `json:"evidence_confidence,omitempty"`
	EvidenceJudgeModel        string   `json:"evidence_judge_model,omitempty"`
	EvidenceEvalSuiteVersion  string   `json:"evidence_eval_suite_version,omitempty"`
	CompletionFeedbackEventID string   `json:"completion_feedback_event_id,omitempty"`
	Confidence                *float64 `json:"confidence,omitempty"`
	Evidence                  string   `json:"evidence,omitempty"`
	Decision                  string   `json:"decision,omitempty"`
	Note                      string   `json:"note,omitempty"`
	JudgeModel                string   `json:"judge_model,omitempty"`
	EvalSuiteVersion          string   `json:"eval_suite_version,omitempty"`
	Evaluator                 struct {
		JudgeModel       string `json:"judge_model,omitempty"`
		EvalSuiteVersion string `json:"eval_suite_version,omitempty"`
		Actor            string `json:"actor,omitempty"`
		Trust            string `json:"trust,omitempty"`
		Type             string `json:"type,omitempty"`
	} `json:"evaluator,omitempty"`
}

func (w deliveryReviewWrapper) evidenceVerdict(run workboard.GateRun) string {
	if run.Executor == workboard.GateRunExecutorHuman {
		return strings.TrimSpace(w.EvidenceVerdict)
	}
	return string(run.State)
}

func (w deliveryReviewWrapper) judgeModel() string {
	if strings.TrimSpace(w.EvidenceJudgeModel) != "" {
		return strings.TrimSpace(w.EvidenceJudgeModel)
	}
	if strings.TrimSpace(w.Evaluator.JudgeModel) != "" {
		return strings.TrimSpace(w.Evaluator.JudgeModel)
	}
	return strings.TrimSpace(w.JudgeModel)
}

func (w deliveryReviewWrapper) evalSuiteVersion() string {
	if strings.TrimSpace(w.EvidenceEvalSuiteVersion) != "" {
		return strings.TrimSpace(w.EvidenceEvalSuiteVersion)
	}
	if strings.TrimSpace(w.Evaluator.EvalSuiteVersion) != "" {
		return strings.TrimSpace(w.Evaluator.EvalSuiteVersion)
	}
	return strings.TrimSpace(w.EvalSuiteVersion)
}

func (w deliveryReviewWrapper) reviewConfidence(run workboard.GateRun) *float64 {
	if run.Executor == workboard.GateRunExecutorHuman && w.EvidenceConfidence != nil {
		return w.EvidenceConfidence
	}
	return w.Confidence
}

func (w deliveryReviewWrapper) actor() string {
	return strings.TrimSpace(w.Evaluator.Actor)
}

type deliveryReviewDetail struct {
	ReasonCode string `json:"reason_code,omitempty"`
	Evidence   []struct {
		Kind string `json:"kind,omitempty"`
	} `json:"evidence,omitempty"`
	Criteria []struct {
		CriterionID         string `json:"criterion_id,omitempty"`
		Text                string `json:"text,omitempty"`
		Verdict             string `json:"verdict,omitempty"`
		Why                 string `json:"why,omitempty"`
		VerificationBinding string `json:"verification_binding,omitempty"`
		TrustTier           string `json:"trust_tier,omitempty"`
	} `json:"criteria"`
	Checks []struct {
		Name   string `json:"name,omitempty"`
		Status string `json:"status,omitempty"`
		Detail string `json:"detail,omitempty"`
	} `json:"checks"`
}

func deliveryReviewAssuranceSources(detail deliveryReviewDetail) []string {
	var repositoryObserved bool
	for _, evidence := range detail.Evidence {
		switch strings.TrimSpace(evidence.Kind) {
		case "pr_merged":
			repositoryObserved = true
		}
	}
	var sources []string
	if repositoryObserved {
		sources = append(sources, "repository_observed")
	}
	return sources
}

func decodeDeliveryReview(raw string) (deliveryReviewWrapper, deliveryReviewDetail) {
	var wrapper deliveryReviewWrapper
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &wrapper)
	var detail deliveryReviewDetail
	if strings.TrimSpace(wrapper.Evidence) != "" {
		_ = json.Unmarshal([]byte(wrapper.Evidence), &detail)
	}
	return wrapper, detail
}

func deliveryRunAuditFields(run workboard.GateRun) (actor string, note string, summary string) {
	wrapper, _ := decodeDeliveryReview(run.EvidenceJSON)
	actor = wrapper.actor()
	note = strings.TrimSpace(wrapper.Note)
	summary = workboard.DeliveryDecisionSummary(run, actor, note)
	return actor, note, summary
}

func deliveryReviewOutstandingMD(run workboard.GateRun, detail deliveryReviewDetail) string {
	if run.State != workboard.NextActionStateFail && run.State != workboard.NextActionStateNeedsHumanReview {
		return ""
	}
	var b strings.Builder
	b.WriteString("_The previous delivery review did not pass. Address these before reporting done again._")
	for _, criterion := range detail.Criteria {
		verdict := strings.TrimSpace(criterion.Verdict)
		if verdict != "unmet" && verdict != "unclear" {
			continue
		}
		label := strings.TrimSpace(criterion.Text)
		if label == "" {
			label = "(criterion)"
		}
		line := fmt.Sprintf("\n- **%s** (%s)", label, verdict)
		if why := strings.TrimSpace(criterion.Why); why != "" {
			line += ": " + why
		}
		b.WriteString(line)
	}
	for _, check := range detail.Checks {
		if !isFailedCheckStatus(check.Status) {
			continue
		}
		line := fmt.Sprintf("\n- **Check failed: %s**", strings.TrimSpace(check.Name))
		if d := strings.TrimSpace(check.Detail); d != "" {
			line += " — " + d
		}
		b.WriteString(line)
	}
	if hint := strings.TrimSpace(run.Hint); hint != "" {
		fmt.Fprintf(&b, "\n\n_Reviewer summary: %s_", hint)
	}
	return strings.TrimSpace(b.String())
}

func isFailedCheckStatus(status string) bool {
	return strings.TrimSpace(status) == "fail"
}

func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func normalizeURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
