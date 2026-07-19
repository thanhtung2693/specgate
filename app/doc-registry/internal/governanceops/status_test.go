package governanceops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// fakeWorkBoardReader is a minimal in-memory WorkBoardReader for tests.
type fakeWorkBoardReader struct {
	crs      []workboard.ChangeRequest
	warnings []workboard.StaleWarning
	criteria map[string][]workboard.AcceptanceCriterion
}

func (f *fakeWorkBoardReader) ListChangeRequests(_ context.Context, includeArchived bool) ([]workboard.ChangeRequest, error) {
	if includeArchived {
		return f.crs, nil
	}
	out := make([]workboard.ChangeRequest, 0, len(f.crs))
	for _, cr := range f.crs {
		if !cr.Archived {
			out = append(out, cr)
		}
	}
	return out, nil
}

func (f *fakeWorkBoardReader) GetChangeRequest(_ context.Context, id string) (*workboard.ChangeRequest, error) {
	for i := range f.crs {
		if f.crs[i].ID == id {
			return &f.crs[i], nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeWorkBoardReader) GetFeature(_ context.Context, _ string) (*workboard.Feature, error) {
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkBoardReader) ListAcceptanceCriteria(_ context.Context, id string) ([]workboard.AcceptanceCriterion, error) {
	return f.criteria[id], nil
}

func (f *fakeWorkBoardReader) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return nil, nil
}

func (f *fakeWorkBoardReader) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return f.warnings, nil
}

// fakeTrackerReader returns one GitHub integration and one tracker link for cr-1.
type fakeTrackerReader struct {
	integrations []integrations.Integration
	links        map[string][]integrations.TrackerLink // keyed by changeRequestID
}

func (f *fakeTrackerReader) List(_ context.Context) ([]integrations.Integration, error) {
	return f.integrations, nil
}

func (f *fakeTrackerReader) ListTrackerLinks(_ context.Context, changeRequestID string) ([]integrations.TrackerLink, error) {
	return f.links[changeRequestID], nil
}

func newStatusTestService() *Service {
	cr := workboard.ChangeRequest{
		ID:    "cr-1",
		Key:   "CR-101",
		Title: "Test CR",
		Phase: workboard.BoardPhaseReady,
	}
	link := integrations.TrackerLink{
		ID:              "link-1",
		IntegrationID:   "intg-1",
		ChangeRequestID: "cr-1",
		ExternalKey:     "42",
		URL:             "https://github.com/acme/shop/issues/42",
		UpdatedAt:       time.Now(),
	}
	return &Service{
		WorkBoard: &fakeWorkBoardReader{crs: []workboard.ChangeRequest{cr}},
		Trackers: &fakeTrackerReader{
			integrations: []integrations.Integration{
				{ID: "intg-1", Provider: "github"},
			},
			links: map[string][]integrations.TrackerLink{
				"cr-1": {link},
			},
		},
	}
}

func TestResolveWorkRefAcceptsIDKeyAndIssueURL(t *testing.T) {
	t.Parallel()
	svc := newStatusTestService()

	for _, ref := range []string{
		"cr-1",
		"CR-101",
		"https://github.com/acme/shop/issues/42",
	} {
		got, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: ref})
		if err != nil {
			t.Fatalf("ResolveWorkRef(%q): %v", ref, err)
		}
		if got.ChangeRequestID != "cr-1" {
			t.Fatalf("ResolveWorkRef(%q) id = %q, want cr-1", ref, got.ChangeRequestID)
		}
	}
}

func TestResolveWorkRefURLRejectsUnknownProviderHint(t *testing.T) {
	t.Parallel()
	svc := newStatusTestService()

	_, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{
		Ref:      "https://github.com/acme/shop/issues/42",
		Provider: "gitlab",
	})
	if err == nil || !strings.Contains(err.Error(), "no integrations configured") {
		t.Fatalf("error = %v, want exact provider validation", err)
	}
}

func TestDeliveryStatusSurfacesHumanOverrideActorAndNote(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{ID: "cr-1", Key: "CR-1", Title: "Human delivery"}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: reader,
		runs: []workboard.GateRun{
			{
				ID:           "run-platform-later",
				Gate:         "delivery_review",
				State:        workboard.NextActionStateFail,
				Hint:         "platform still disagrees",
				Executor:     workboard.GateRunExecutorPlatform,
				EvidenceJSON: `{"verdict":"fail"}`,
				CreatedAt:    time.Unix(200, 0).UTC(),
			},
			{
				ID:       "run-human",
				Gate:     "delivery_review",
				State:    workboard.NextActionStatePass,
				Hint:     "delivery accepted by lead@example.com: false failure reviewed",
				Executor: workboard.GateRunExecutorHuman,
				EvidenceJSON: `{
					"verdict":"pass",
					"confidence":1,
					"evaluator":{"type":"human_decision","actor":"lead@example.com","trust":"human_decision"},
					"decision":"approve",
					"note":"false failure reviewed"
				}`,
				CreatedAt: time.Unix(100, 0).UTC(),
			},
		},
	}
	svc := &Service{WorkBoard: readerWithRuns}

	got, err := svc.DeliveryStatus(context.Background(), DeliveryStatusInput{ChangeRequestID: "cr-1"})
	if err != nil {
		t.Fatalf("DeliveryStatus: %v", err)
	}
	if got.GateRunID != "run-human" || got.Verdict != "pass" {
		t.Fatalf("status = %+v, want human run to outrank later platform run", got)
	}
	if got.Executor != workboard.GateRunExecutorHuman || got.Actor != "lead@example.com" ||
		got.Note != "false failure reviewed" || !strings.Contains(got.Summary, "delivery accepted by lead@example.com") {
		t.Fatalf("human decision audit fields missing: %+v", got)
	}
}

func TestDeliveryStatusSeparatesHumanDecisionFromEvidenceVerdict(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
		ID: "cr-1", Key: "CR-1", Title: "Separate evidence and authority",
	}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: reader,
		runs: []workboard.GateRun{{
			ID:       "run-human-reject",
			Gate:     "delivery_review",
			State:    workboard.NextActionStateFail,
			Executor: workboard.GateRunExecutorHuman,
			EvidenceJSON: `{
				"verdict":"fail",
				"evidence_verdict":"pass",
				"evidence_judge_model":"gpt-5.2",
				"evidence_eval_suite_version":"delivery-review-v1",
				"confidence":1,
				"evidence_confidence":0,
				"decision":"reject",
				"evaluator":{"actor":"lead@example.com"}
			}`,
			CreatedAt: time.Unix(100, 0).UTC(),
		}},
	}

	got, err := (&Service{WorkBoard: readerWithRuns}).DeliveryStatus(
		context.Background(),
		DeliveryStatusInput{ChangeRequestID: "cr-1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "fail" || got.EvidenceVerdict != "pass" ||
		got.Executor != workboard.GateRunExecutorHuman ||
		got.JudgeModel != "gpt-5.2" || got.EvalSuite != "delivery-review-v1" ||
		got.Confidence == nil || *got.Confidence != 0 {
		t.Fatalf("status = %+v, want rejected decision over passing evidence", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"confidence":0`) {
		t.Fatalf("zero evidence confidence was omitted: %s", raw)
	}
}

type fakeAuthoritativeDeliveryReader struct {
	*fakeWorkBoardReaderWithRuns
	authoritative *workboard.GateRun
}

func (f *fakeAuthoritativeDeliveryReader) AuthoritativeDeliveryReviewRun(
	_ context.Context,
	_ string,
) (*workboard.GateRun, error) {
	return f.authoritative, nil
}

func TestDeliveryStatusUsesAuthoritativeQueryBeyondMixedGateLimit(t *testing.T) {
	t.Parallel()
	base := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
		ID: "cr-1", Key: "CR-1", Title: "Preserve human authority",
	}}}
	human := &workboard.GateRun{
		ID:           "run-human-old",
		Gate:         "delivery_review",
		State:        workboard.NextActionStatePass,
		Executor:     workboard.GateRunExecutorHuman,
		EvidenceJSON: `{"evaluator":{"actor":"lead@example.com"}}`,
		CreatedAt:    time.Unix(100, 0).UTC(),
	}
	mixedWindow := make([]workboard.GateRun, 50)
	for i := range mixedWindow {
		mixedWindow[i] = workboard.GateRun{
			ID:        fmt.Sprintf("run-%d", i),
			Gate:      "delivery_review",
			State:     workboard.NextActionStateFail,
			Executor:  workboard.GateRunExecutorPlatform,
			CreatedAt: time.Unix(int64(200+i), 0).UTC(),
		}
	}
	reader := &fakeAuthoritativeDeliveryReader{
		fakeWorkBoardReaderWithRuns: &fakeWorkBoardReaderWithRuns{
			fakeWorkBoardReader: base,
			runs:                mixedWindow,
		},
		authoritative: human,
	}

	got, err := (&Service{WorkBoard: reader}).DeliveryStatus(
		context.Background(),
		DeliveryStatusInput{ChangeRequestID: "cr-1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.GateRunID != human.ID || got.Executor != workboard.GateRunExecutorHuman {
		t.Fatalf("status = %+v, want authoritative human run beyond mixed history window", got)
	}
}

func TestDeliveryStatusSurfacesPerCriterionTrustTier(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{ID: "cr-1", Key: "CR-1", Title: "Grounded delivery"}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: reader,
		runs: []workboard.GateRun{
			{
				ID:       "run-platform",
				Gate:     "delivery_review",
				State:    workboard.NextActionStatePass,
				Hint:     "grounded evidence",
				Executor: workboard.GateRunExecutorPlatform,
				EvidenceJSON: `{
					"verdict":"pass",
					"confidence":0.9,
					"evidence":"{\"criteria\":[{\"criterion_id\":\"ac-1\",\"text\":\"Handler exists\",\"verdict\":\"met\",\"trust_tier\":\"grounded\"}],\"checks\":[]}"
				}`,
				CreatedAt: time.Unix(100, 0).UTC(),
			},
		},
	}
	svc := &Service{WorkBoard: readerWithRuns}

	got, err := svc.DeliveryStatus(context.Background(), DeliveryStatusInput{ChangeRequestID: "cr-1", Detail: true})
	if err != nil {
		t.Fatalf("DeliveryStatus: %v", err)
	}
	if len(got.PerCriterion) != 1 {
		t.Fatalf("PerCriterion = %#v", got.PerCriterion)
	}
	if got.PerCriterion[0].TrustTier != "grounded" {
		t.Fatalf("TrustTier = %q, want grounded", got.PerCriterion[0].TrustTier)
	}
}

func TestDeliveryStatusProjectsReasonCode(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
		ID: "cr-1", Key: "CR-1", Title: "Policy unavailable",
	}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: reader,
		runs: []workboard.GateRun{{
			ID:       "run-policy",
			Gate:     "delivery_review",
			State:    workboard.NextActionStateNeedsHumanReview,
			Hint:     "Governing delivery policy is unavailable.",
			Executor: workboard.GateRunExecutorPlatform,
			EvidenceJSON: `{
				"evidence":"{\"reason_code\":\"policy_unavailable\",\"criteria\":[],\"checks\":[]}"
			}`,
			CreatedAt: time.Unix(100, 0).UTC(),
		}},
	}
	svc := &Service{WorkBoard: readerWithRuns}

	got, err := svc.DeliveryStatus(
		context.Background(),
		DeliveryStatusInput{ChangeRequestID: "cr-1", Detail: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasonCode != "policy_unavailable" {
		t.Fatalf("ReasonCode = %q, want policy_unavailable", got.ReasonCode)
	}
}

func TestDeliveryStatusProjectsLatestCompletionGitReceipt(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{ID: "cr-1", Key: "CR-1", Title: "Receipt delivery"}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: reader,
		runs: []workboard.GateRun{{
			ID:           "run-platform",
			Gate:         "delivery_review",
			State:        workboard.NextActionStatePass,
			Hint:         "delivery passed",
			Executor:     workboard.GateRunExecutorPlatform,
			EvidenceJSON: `{"verdict":"pass"}`,
			CreatedAt:    time.Unix(100, 0).UTC(),
		}},
	}
	receipt := &GitReceipt{
		Repository:   "https://github.com/acme/project.git",
		Availability: "available",
		Branch:       "feature/receipt",
		BaseRevision: "base123",
		HeadRevision: "head456",
		ChangedFiles: []string{"app.go"},
		DiffDigest:   "sha256:abc123",
		Warnings:     []string{},
	}
	payload, err := json.Marshal(ReportFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented and verified.",
		GitReceipt:      receipt,
	})
	if err != nil {
		t.Fatalf("marshal completion payload: %v", err)
	}
	feedback := &fakeFeedbackStore{created: []integrations.GovernanceFeedbackEvent{{
		ID:              "feedback-1",
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON:     string(payload),
		CreatedAt:       time.Unix(90, 0).UTC(),
	}}}
	svc := &Service{WorkBoard: readerWithRuns, FeedbackStore: feedback}

	got, err := svc.DeliveryStatus(context.Background(), DeliveryStatusInput{ChangeRequestID: "cr-1", Detail: true})
	if err != nil {
		t.Fatalf("DeliveryStatus: %v", err)
	}
	if got.GitReceipt == nil {
		t.Fatal("GitReceipt is nil, want latest completion receipt")
	}
	if !reflect.DeepEqual(got.GitReceipt, receipt) {
		t.Fatalf("GitReceipt = %#v, want %#v", got.GitReceipt, receipt)
	}
}

func TestDeliveryStatusMarksReviewOutdatedForNewerCompletion(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
		ID: "cr-1", Key: "CR-1", Title: "Review the newest completion",
	}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: reader,
		runs: []workboard.GateRun{{
			ID: "review-completion-1", Gate: "delivery_review",
			State: workboard.NextActionStatePass, Executor: workboard.GateRunExecutorPlatform,
			EvidenceJSON: `{"completion_feedback_event_id":"completion-1","verdict":"pass"}`,
			CreatedAt:    time.Unix(100, 0).UTC(),
		}},
	}
	latestReceipt := &GitReceipt{
		Availability: "available",
		HeadRevision: "new-completion-head",
		ChangedFiles: []string{"new.go"},
	}
	payload, err := json.Marshal(ReportFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Summary:         "Corrected implementation.",
		GitReceipt:      latestReceipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	feedback := &fakeFeedbackStore{created: []integrations.GovernanceFeedbackEvent{{
		ID:              "completion-2",
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON:     string(payload),
		CreatedAt:       time.Unix(200, 0).UTC(),
	}}}

	got, err := (&Service{WorkBoard: readerWithRuns, FeedbackStore: feedback}).DeliveryStatus(
		context.Background(),
		DeliveryStatusInput{ChangeRequestID: "cr-1", Detail: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasonCode != "delivery_review_outdated" ||
		got.Verdict != string(workboard.NextActionStateNeedsHumanReview) ||
		got.GateRunID != "" ||
		got.Executor != "" {
		t.Fatalf("status = %+v, want an unreviewed latest-completion state", got)
	}
	if !reflect.DeepEqual(got.GitReceipt, latestReceipt) {
		t.Fatalf("receipt = %#v, want latest completion %#v", got.GitReceipt, latestReceipt)
	}
}

func TestDeliveryStatusMarksCompletionWithoutReviewOutdated(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
			ID: "cr-1", Key: "CR-1", Title: "Review failed after completion",
		}}},
	}
	receipt := &GitReceipt{Availability: "available", HeadRevision: "completion-head"}
	payload, err := json.Marshal(ReportFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Summary:         "Completion persisted before review failed.",
		GitReceipt:      receipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	feedback := &fakeFeedbackStore{created: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1",
		EventType:   integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: string(payload), CreatedAt: time.Unix(200, 0).UTC(),
	}}}

	got, err := (&Service{WorkBoard: reader, FeedbackStore: feedback}).DeliveryStatus(
		context.Background(),
		DeliveryStatusInput{ChangeRequestID: "cr-1", Detail: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.ReasonCode != "delivery_review_outdated" ||
		got.Verdict != string(workboard.NextActionStateNeedsHumanReview) {
		t.Fatalf("status = %+v, want pending review for persisted completion", got)
	}
	if !reflect.DeepEqual(got.GitReceipt, receipt) {
		t.Fatalf("receipt = %#v, want %#v", got.GitReceipt, receipt)
	}
}

func TestDeliveryStatusMarksUnboundPlatformReviewOutdatedWhenCompletionExists(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
		ID: "cr-1", Key: "CR-1", Title: "Do not trust an unbound review",
	}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: reader,
		runs: []workboard.GateRun{{
			ID: "legacy-unbound-platform", Gate: "delivery_review",
			State: workboard.NextActionStatePass, Executor: workboard.GateRunExecutorPlatform,
			CreatedAt: time.Unix(100, 0).UTC(),
		}},
	}
	feedback := &fakeFeedbackStore{created: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1",
		EventType:   integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"summary":"Implemented.","agent":{"name":"builder"}}`,
		CreatedAt:   time.Unix(200, 0).UTC(),
	}}}

	got, err := (&Service{WorkBoard: readerWithRuns, FeedbackStore: feedback}).DeliveryStatus(
		context.Background(),
		DeliveryStatusInput{ChangeRequestID: "cr-1", Detail: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasonCode != "delivery_review_outdated" ||
		got.Verdict != string(workboard.NextActionStateNeedsHumanReview) ||
		got.GateRunID != "" {
		t.Fatalf("status = %+v, want unreviewed latest-completion state", got)
	}
}

func TestDeliveryStatusProjectsPeerReviewStateForLatestCompletion(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{ID: "cr-1", Key: "CR-1", Title: "Peer reviewed delivery"}}}
	readerWithRuns := &fakeWorkBoardReaderWithRuns{fakeWorkBoardReader: reader, runs: []workboard.GateRun{{
		ID: "run-platform", Gate: "delivery_review", State: workboard.NextActionStatePass, Executor: workboard.GateRunExecutorPlatform, CreatedAt: time.Unix(100, 0).UTC(),
	}}}
	receipt := &GitReceipt{Repository: "specgate", HeadRevision: "abc"}
	completion, err := json.Marshal(ReportFeedbackInput{ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentCompleted, Summary: "Implemented.", Agent: FeedbackAgent{Name: "builder"}, GitReceipt: receipt})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := json.Marshal(ReportFeedbackInput{
		ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentPeerReviewed, Summary: "Reviewed.", Agent: FeedbackAgent{Name: "reviewer"},
		PeerReviewOf: &PeerReviewBinding{CompletionFeedbackEventID: "completion-1", GitReceipt: receipt},
		Criteria:     []FeedbackCriterion{{CriterionID: "ac-1", Claim: "satisfied", Evidence: &FeedbackEvidence{Kind: "file", Path: "implementation.go"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stalePeer, err := json.Marshal(ReportFeedbackInput{
		ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentPeerReviewed, Summary: "Older review.", Agent: FeedbackAgent{Name: "older-reviewer"},
		PeerReviewOf: &PeerReviewBinding{CompletionFeedbackEventID: "completion-1", GitReceipt: receipt},
		Criteria:     []FeedbackCriterion{{CriterionID: "ac-1", Claim: "partial"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	feedback := &fakeFeedbackStore{created: []integrations.GovernanceFeedbackEvent{
		{ID: "completion-1", ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentCompleted, PayloadJSON: string(completion), CreatedAt: time.Unix(90, 0).UTC()},
		{ID: "peer-a", ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentPeerReviewed, PayloadJSON: string(stalePeer), CreatedAt: time.Unix(95, 0).UTC()},
		{ID: "peer-z", ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentPeerReviewed, PayloadJSON: string(peer), CreatedAt: time.Unix(95, 0).UTC()},
	}}
	svc := &Service{WorkBoard: readerWithRuns, FeedbackStore: feedback}
	got, err := svc.DeliveryStatus(context.Background(), DeliveryStatusInput{ChangeRequestID: "cr-1", Detail: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.PeerReview.State != "passed" || got.PeerReview.AgentName != "reviewer" {
		t.Fatalf("peer review = %#v", got.PeerReview)
	}
}

func TestLatestDeliveryRunBreaksTimestampTiesByID(t *testing.T) {
	t.Parallel()
	createdAt := time.Unix(100, 0).UTC()
	got := latestDeliveryRun([]workboard.GateRun{
		{
			ID: "run-a", Gate: "delivery_review", State: workboard.NextActionStateFail,
			Executor: workboard.GateRunExecutorPlatform, CreatedAt: createdAt,
		},
		{
			ID: "run-z", Gate: "delivery_review", State: workboard.NextActionStatePass,
			Executor: workboard.GateRunExecutorPlatform, CreatedAt: createdAt,
		},
	})
	if got == nil || got.ID != "run-z" {
		t.Fatalf("latest delivery run = %#v, want run-z", got)
	}
}

func TestDeliveryStatusExposesRepositoryObservationOnly(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkBoardReaderWithRuns{
		fakeWorkBoardReader: &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
			ID: "cr-1", Key: "CR-1", Title: "Corroborated delivery",
		}}},
		runs: []workboard.GateRun{{
			ID:       "run-platform",
			Gate:     "delivery_review",
			State:    workboard.NextActionStatePass,
			Executor: workboard.GateRunExecutorPlatform,
			EvidenceJSON: `{
				"verdict":"pass",
				"evidence":"{\"evidence\":[{\"kind\":\"pr_merged\"},{\"kind\":\"unknown\"}]}"
			}`,
			CreatedAt: time.Unix(100, 0).UTC(),
		}},
	}

	got, err := (&Service{WorkBoard: reader}).DeliveryStatus(
		context.Background(),
		DeliveryStatusInput{ChangeRequestID: "cr-1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body["assurance_sources"], []any{"repository_observed"}) {
		t.Fatalf("assurance_sources = %#v", body["assurance_sources"])
	}
}

func TestDeliveryReviewAssuranceSourcesIgnoresCIEvents(t *testing.T) {
	detail := deliveryReviewDetail{}
	detail.Evidence = append(detail.Evidence, struct {
		Kind string `json:"kind,omitempty"`
	}{Kind: "ci_run"})

	if sources := deliveryReviewAssuranceSources(detail); len(sources) != 0 {
		t.Fatalf("assurance_sources = %#v, want no CI assurance", sources)
	}
}

type fakeWorkBoardReaderWithRuns struct {
	*fakeWorkBoardReader
	runs []workboard.GateRun
}

func (f *fakeWorkBoardReaderWithRuns) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return f.runs, nil
}

func TestResolveWorkRefAcceptsArchivedKey(t *testing.T) {
	t.Parallel()
	svc := &Service{WorkBoard: &fakeWorkBoardReader{crs: []workboard.ChangeRequest{
		{
			ID:       "cr-archived",
			Key:      "CR-ARCHIVED",
			Title:    "Archived CR",
			Phase:    workboard.BoardPhaseDelivered,
			Archived: true,
		},
	}}}

	got, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: "CR-ARCHIVED"})
	if err != nil {
		t.Fatalf("ResolveWorkRef archived key: %v", err)
	}
	if got.ChangeRequestID != "cr-archived" {
		t.Fatalf("change_request_id = %q, want cr-archived", got.ChangeRequestID)
	}
}

func TestResolveWorkRefBareIssueKeyWithoutProviderReturnsNotFound(t *testing.T) {
	t.Parallel()
	svc := newStatusTestService()
	_, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: "SHOP-42"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestResolveWorkRefUnknownKeyReturnsNotFound(t *testing.T) {
	t.Parallel()
	svc := newStatusTestService()
	_, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: "CR-MISSING"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestGovernanceStatusCountsDeliveredAndExcludesItFromAttention(t *testing.T) {
	t.Parallel()
	svc := &Service{WorkBoard: &fakeWorkBoardReader{
		crs: []workboard.ChangeRequest{
			{ID: "cr-delivered", Key: "CR-DELIVERED", Title: "Delivered work", Phase: workboard.BoardPhaseDelivered},
			{ID: "cr-ready", Key: "CR-READY", Title: "Ready work", Phase: workboard.BoardPhaseReady},
		},
		warnings: []workboard.StaleWarning{
			// A delivered CR with a stale warning must NOT appear in attention.
			{Code: workboard.WarningDeliveryStale, ChangeRequestID: "cr-delivered", Severity: "warning"},
		},
	}}

	got, err := svc.GovernanceStatus(context.Background(), GovernanceStatusInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Counts.Delivered != 1 {
		t.Fatalf("counts.delivered = %d, want 1", got.Counts.Delivered)
	}
	if got.Counts.Ready != 1 {
		t.Fatalf("counts.ready = %d, want 1 (delivered must not count as ready)", got.Counts.Ready)
	}
	if got.Counts.Total != 2 {
		t.Fatalf("counts.total = %d, want 2", got.Counts.Total)
	}
	if len(got.Attention) != 0 {
		t.Fatalf("attention = %+v, want no stale-pack attention", got.Attention)
	}
	if !strings.Contains(got.Summary, "1 delivered") {
		t.Fatalf("summary = %q, want it to mention delivered", got.Summary)
	}
}
