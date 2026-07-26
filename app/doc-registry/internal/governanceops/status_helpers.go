package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// humanActionGates are gates whose pending state requires a human to act in the
// web UI — the agent cannot perform these actions.
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
	parts := make([]string, 0, 5)
	if counts.Intake > 0 {
		parts = append(parts, fmt.Sprintf("%d in intake", counts.Intake))
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
