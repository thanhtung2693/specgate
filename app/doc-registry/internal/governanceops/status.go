package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// humanActionGates are gates whose pending state requires a human to act in the
// web UI — the agent cannot perform these actions.
var humanActionGates = map[string]string{
	"canonical_spec": "Promote spec to canonical",
	"delivery_pack":  "Attach delivery context pack",
}

type resolveMatch struct {
	cr   workboard.ChangeRequest
	link integrations.TrackerLink
}

// ResolveWorkRef resolves a flexible work reference to its SpecGate change
// request. Resolution order:
//  1. Direct change-request ID via GetChangeRequest.
//  2. Case-insensitive Key scan across active CRs.
//  3. Full HTTPS URL → infer provider from host, match tracker link URL.
//  4. Bare tracker key → explicit Provider required.
func (s *Service) ResolveWorkRef(ctx context.Context, in ResolveWorkRefInput) (ResolvedWork, error) {
	if s.WorkBoard == nil {
		return ResolvedWork{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	ref := strings.TrimSpace(in.Ref)
	provider := strings.ToLower(strings.TrimSpace(in.Provider))

	// 0. Accept the specgate://context-pack/<cr> URI the system emits (e.g. in
	// `work show` output) so it round-trips as a `work context` ref.
	if crID, ok := contextPackURIChangeRequestID(ref); ok {
		ref = crID
	}

	// 1. Direct ID lookup.
	if cr, err := s.WorkBoard.GetChangeRequest(ctx, ref); err == nil {
		return crToResolved(cr, integrations.TrackerLink{}), nil
	}

	// 2. Case-insensitive Key scan (also loads CRs for later steps).
	crs, err := s.WorkBoard.ListChangeRequests(ctx, false)
	if err != nil {
		return ResolvedWork{}, err
	}
	refUpper := strings.ToUpper(ref)
	for i := range crs {
		if strings.ToUpper(strings.TrimSpace(crs[i].Key)) == refUpper {
			return crToResolved(&crs[i], integrations.TrackerLink{}), nil
		}
	}

	// 3. Full HTTPS URL → infer provider and match tracker link.
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return s.resolveByURL(ctx, crs, ref, provider)
	}

	// 4. Bare tracker key requires Provider.
	if provider == "" {
		return ResolvedWork{}, ErrProviderRequired
	}
	return s.resolveByTrackerKey(ctx, crs, ref, provider)
}

// contextPackURIChangeRequestID extracts the change-request id from a
// specgate://context-pack/{cr}[/fe|/be] URI (mirrors the emitter in
// internal/mcp/context_pack_resources.go; kept local to avoid a package cycle).
// It returns ("", false) for the artifact-scoped variant
// (specgate://context-pack/artifact/{id}) and any other form, so those fall
// through to normal resolution unchanged. The fe/be lane suffix resolves the CR
// but does not select the pack lane — that stays driven by the --lane flag.
func contextPackURIChangeRequestID(ref string) (string, bool) {
	const prefix = "specgate://context-pack/"
	rest, ok := strings.CutPrefix(ref, prefix)
	if !ok || rest == "" {
		return "", false
	}
	parts := strings.Split(rest, "/")
	switch len(parts) {
	case 1:
		if parts[0] == "" || parts[0] == "artifact" {
			return "", false
		}
		return parts[0], true
	case 2:
		if parts[0] == "" || parts[0] == "artifact" || (parts[1] != "fe" && parts[1] != "be") {
			return "", false
		}
		return parts[0], true
	default:
		return "", false
	}
}

func (s *Service) resolveByURL(ctx context.Context, crs []workboard.ChangeRequest, rawURL, providerHint string) (ResolvedWork, error) {
	if s.Trackers == nil {
		return ResolvedWork{}, fmt.Errorf("%w: tracker links not configured", ErrUnavailable)
	}
	allIntegrations, err := s.Trackers.List(ctx)
	if err != nil {
		return ResolvedWork{}, err
	}

	inferredProviders := inferProvidersFromURL(rawURL, allIntegrations)

	allowed := map[string]struct{}{}
	for _, intg := range allIntegrations {
		p := strings.ToLower(intg.Provider)
		for _, ip := range inferredProviders {
			if ip == p && (providerHint == "" || p == providerHint) {
				allowed[intg.ID] = struct{}{}
			}
		}
	}
	// Fall back to hint-only if inference found nothing.
	if len(allowed) == 0 && providerHint != "" {
		for _, intg := range allIntegrations {
			if strings.EqualFold(intg.Provider, providerHint) {
				allowed[intg.ID] = struct{}{}
			}
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
		case workboard.BoardPhaseHandoff:
			result.Counts.Handoff++
		case workboard.BoardPhaseDelivered:
			result.Counts.Delivered++
		default:
			result.Counts.Intake++
		}
		result.Counts.Total++

		// Delivered items passed their latest delivery review — nothing left to
		// act on, so they never surface in attention even with stale warnings.
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

// ListWorkItems returns ready/handed-off work items matching the filter.
func (s *Service) ListWorkItems(ctx context.Context, in ListWorkItemsInput) (ListWorkItemsResult, error) {
	if s.WorkBoard == nil {
		return ListWorkItemsResult{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	if in.Mine {
		return ListWorkItemsResult{}, fmt.Errorf("mine filter is not supported yet")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	workTypeFilter := strings.ToLower(strings.TrimSpace(in.WorkType))
	workspaceID := strings.TrimSpace(in.WorkspaceID)

	items, err := s.WorkBoard.ListChangeRequests(ctx, false)
	if err != nil {
		return ListWorkItemsResult{}, err
	}

	filtered := make([]WorkItemSummary, 0, len(items))
	for _, item := range items {
		if workspaceID != "" && strings.TrimSpace(item.WorkspaceID) != workspaceID {
			continue
		}
		phase := item.Phase
		if phase == "" {
			phase = item.DerivePhase()
		}
		if !includeWorkItemPhase(phase, in.Ready, in.HandedOff) {
			continue
		}
		if workTypeFilter != "" && strings.ToLower(string(item.WorkType)) != workTypeFilter {
			continue
		}
		filtered = append(filtered, WorkItemSummary{
			ChangeRequestID:  item.ID,
			ChangeRequestKey: item.Key,
			FeatureID:        item.FeatureID,
			Title:            item.Title,
			Phase:            string(phase),
			ContextPackURI:   contextPackURI(item.ID, ""),
			WorkType:         string(item.WorkType),
		})
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return ListWorkItemsResult{Items: filtered}, nil
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
		"ac_coverage", "execution_brief", "canonical_spec", "delivery_pack",
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
	if latest := latestDeliveryRun(runs); latest != nil {
		deliveryReview = &DeliveryReviewSummary{
			Verdict:    string(latest.State),
			Hint:       latest.Hint,
			ReviewedAt: formatRFC3339(latest.CreatedAt),
		}
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

// DeliveryStatus returns the latest delivery-review verdict for a CR.
func (s *Service) DeliveryStatus(ctx context.Context, in DeliveryStatusInput) (DeliveryStatusResult, error) {
	if s.WorkBoard == nil {
		return DeliveryStatusResult{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(in.ChangeRequestID)
	if id == "" {
		return DeliveryStatusResult{}, fmt.Errorf("change_request_id is required")
	}

	runs, err := s.WorkBoard.ListGateRuns(ctx, id, 50)
	if err != nil {
		return DeliveryStatusResult{}, err
	}

	latest := latestDeliveryRun(runs)
	if latest == nil {
		return DeliveryStatusResult{ChangeRequestID: id, Found: false}, nil
	}

	wrapper, detail := decodeDeliveryReview(latest.EvidenceJSON)
	result := DeliveryStatusResult{
		ChangeRequestID: id,
		GateRunID:       latest.ID,
		Found:           true,
		Verdict:         string(latest.State),
		Hint:            latest.Hint,
		Confidence:      wrapper.Confidence,
		ReviewedAt:      formatRFC3339(latest.CreatedAt),
		OutstandingMD:   deliveryReviewOutstandingMD(*latest, detail),
	}
	if in.Detail {
		for _, c := range detail.Criteria {
			result.PerCriterion = append(result.PerCriterion, CriterionReview{
				CriterionID: c.CriterionID,
				Text:        c.Text,
				Verdict:     c.Verdict,
				Why:         c.Why,
			})
		}
		for _, c := range detail.Checks {
			result.Checks = append(result.Checks, CheckResult{Name: c.Name, Status: c.Status, Detail: c.Detail})
		}
	}
	return result, nil
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
		ContextPackURI:   contextPackURI(cr.ID, link.Lane),
		IssueKey:         link.ExternalKey,
		IssueURL:         link.URL,
		Lane:             link.Lane,
	}
}

func contextPackURI(changeRequestID, lane string) string {
	uri := "specgate://context-pack/" + strings.TrimSpace(changeRequestID)
	if lane == "fe" || lane == "be" {
		uri += "/" + lane
	}
	return uri
}

func includeWorkItemPhase(phase workboard.BoardPhase, ready, handedOff bool) bool {
	switch {
	case ready && !handedOff:
		return phase == workboard.BoardPhaseReady
	case handedOff && !ready:
		return phase == workboard.BoardPhaseHandoff
	default:
		return phase == workboard.BoardPhaseReady || phase == workboard.BoardPhaseHandoff
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
	if counts.Handoff > 0 {
		parts = append(parts, fmt.Sprintf("%d in handoff", counts.Handoff))
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
	var latest *workboard.GateRun
	for i := range runs {
		if runs[i].Gate != "delivery_review" {
			continue
		}
		if latest == nil || runs[i].CreatedAt.After(latest.CreatedAt) {
			cp := runs[i]
			latest = &cp
		}
	}
	return latest
}

type deliveryReviewWrapper struct {
	EvidenceContractVersion string  `json:"evidence_contract_version,omitempty"`
	Verdict                 string  `json:"verdict,omitempty"`
	Confidence              float64 `json:"confidence,omitempty"`
	Evidence                string  `json:"evidence,omitempty"`
}

type deliveryReviewDetail struct {
	Criteria []struct {
		CriterionID string `json:"criterion_id,omitempty"`
		Text        string `json:"text,omitempty"`
		Verdict     string `json:"verdict,omitempty"`
		Why         string `json:"why,omitempty"`
	} `json:"criteria"`
	Checks []struct {
		Name   string `json:"name,omitempty"`
		Status string `json:"status,omitempty"`
		Detail string `json:"detail,omitempty"`
	} `json:"checks"`
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

func deliveryReviewOutstandingMD(run workboard.GateRun, detail deliveryReviewDetail) string {
	if run.State != workboard.NextActionStateFail {
		return ""
	}
	var b strings.Builder
	b.WriteString("_The previous delivery review did not pass. Address these before reporting done again._")
	for _, criterion := range detail.Criteria {
		verdict := strings.ToLower(strings.TrimSpace(criterion.Verdict))
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
		if !strings.EqualFold(strings.TrimSpace(check.Status), "fail") {
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

func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func normalizeURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// inferProvidersFromURL returns the provider names implied by the URL host,
// checking both well-known domains and integration BaseURLs for self-hosted.
func inferProvidersFromURL(rawURL string, integrations []integrations.Integration) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())

	var providers []string
	add := func(p string) {
		for _, ep := range providers {
			if ep == p {
				return
			}
		}
		providers = append(providers, p)
	}

	switch {
	case strings.HasSuffix(host, "github.com"):
		add("github")
	case strings.HasSuffix(host, "gitlab.com"):
		add("gitlab")
	case strings.HasSuffix(host, "linear.app"):
		add("linear")
	}

	for _, intg := range integrations {
		if intg.BaseURL == "" {
			continue
		}
		bu, err := url.Parse(intg.BaseURL)
		if err != nil {
			continue
		}
		if strings.EqualFold(bu.Hostname(), host) {
			add(strings.ToLower(intg.Provider))
		}
	}
	return providers
}
