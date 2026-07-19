package governanceops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// --- fakes spanning every audit source ---

type fakeAuditWorkBoard struct {
	cr            *workboard.ChangeRequest
	feature       *workboard.Feature
	gateRuns      []workboard.GateRun
	featureErr    error
	gateErr       error
	lastGateLimit int
}

func (f *fakeAuditWorkBoard) ListChangeRequests(context.Context, bool) ([]workboard.ChangeRequest, error) {
	return []workboard.ChangeRequest{*f.cr}, nil
}

func (f *fakeAuditWorkBoard) GetChangeRequest(_ context.Context, id string) (*workboard.ChangeRequest, error) {
	if f.cr != nil && f.cr.ID == id {
		return f.cr, nil
	}
	return nil, ErrNotFound
}

func (f *fakeAuditWorkBoard) GetFeature(_ context.Context, _ string) (*workboard.Feature, error) {
	if f.featureErr != nil {
		return nil, f.featureErr
	}
	if f.feature == nil {
		return nil, workboard.ErrNotFound
	}
	return f.feature, nil
}

func (f *fakeAuditWorkBoard) ListAcceptanceCriteria(context.Context, string) ([]workboard.AcceptanceCriterion, error) {
	return nil, nil
}

func (f *fakeAuditWorkBoard) ListGateRuns(_ context.Context, _ string, limit int) ([]workboard.GateRun, error) {
	f.lastGateLimit = limit
	return f.gateRuns, f.gateErr
}

func (f *fakeAuditWorkBoard) ListStaleWarnings(context.Context, workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return nil, nil
}

type fakeAuditEvents struct {
	byArtifact map[string][]artifact.Event
	err        error
	lastLimit  int
}

func (f *fakeAuditEvents) ListEvents(_ context.Context, filter artifact.EventFilter) ([]artifact.Event, error) {
	f.lastLimit = filter.Limit
	return f.byArtifact[filter.ArtifactID], f.err
}

type fakeAuditReadiness struct {
	byArtifact map[string][]artifact.ReadinessRun
	err        error
	lastLimit  int
}

func (f *fakeAuditReadiness) ListReadinessRuns(_ context.Context, artifactID string, limit int) ([]artifact.ReadinessRun, error) {
	f.lastLimit = limit
	return f.byArtifact[artifactID], f.err
}

type fakeAuditLifecycle struct {
	byEntity  map[string][]workboard.LifecycleEvent // key: kind+"/"+id
	err       error
	lastLimit int
}

func (f *fakeAuditLifecycle) ListLifecycleEvents(_ context.Context, kind, id string, limit int) ([]workboard.LifecycleEvent, error) {
	f.lastLimit = limit
	return f.byEntity[kind+"/"+id], f.err
}

func TestAuditTrail_RequestsCompleteHistoryFromEverySource(t *testing.T) {
	t.Parallel()
	cr := &workboard.ChangeRequest{ID: "cr-1", Key: "CR-1", LeadArtifactID: "art-1"}
	workBoard := &fakeAuditWorkBoard{cr: cr}
	events := &fakeAuditEvents{byArtifact: map[string][]artifact.Event{"art-1": {}}}
	readiness := &fakeAuditReadiness{byArtifact: map[string][]artifact.ReadinessRun{"art-1": {}}}
	lifecycle := &fakeAuditLifecycle{byEntity: map[string][]workboard.LifecycleEvent{"change_request/cr-1": {}}}
	svc := &Service{WorkBoard: workBoard, AuditEvents: events, ReadinessRuns: readiness, AuditLifecycle: lifecycle}

	if _, err := svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "CR-1"}, true); err != nil {
		t.Fatalf("AuditTrail: %v", err)
	}
	if events.lastLimit != -1 || readiness.lastLimit != -1 || workBoard.lastGateLimit != -1 || lifecycle.lastLimit != -1 {
		t.Fatalf("limits = events:%d readiness:%d gates:%d lifecycle:%d, want -1 for complete history", events.lastLimit, readiness.lastLimit, workBoard.lastGateLimit, lifecycle.lastLimit)
	}
}

func TestArtifactAuditActionUsesExplicitEventType(t *testing.T) {
	t.Parallel()

	event := artifact.Event{EventType: artifact.EventApproved}
	if got := artifactAuditAction(event); got != "approved" {
		t.Fatalf("action = %q, want approved", got)
	}
}

func TestAuditTrail_MergesSourcesSortedAscendingWithDerivedTrust(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := time.Date(2026, 7, 9, 8, 0, 0, 0, time.UTC)

	cr := &workboard.ChangeRequest{
		ID:             "cr-1",
		Key:            "CR-1",
		FeatureID:      "feat-1",
		Title:          "Add audit trail",
		LeadArtifactID: "art-lead",
		DeliveryReview: &workboard.DeliveryReviewSnapshot{
			Verdict:    "pass",
			Executor:   workboard.GateRunExecutorHuman,
			Actor:      "carol",
			Note:       "looks good",
			ReviewedAt: base.Add(5 * time.Hour),
		},
	}
	feature := &workboard.Feature{ID: "feat-1", Key: "FEAT-1", Name: "Audit", CanonicalArtifactID: "art-canon"}

	svc := &Service{
		WorkBoard: &fakeAuditWorkBoard{
			cr:      cr,
			feature: feature,
			gateRuns: []workboard.GateRun{
				// latest delivery_review — deduped against snapshot (same instant)
				{Gate: "delivery_review", State: workboard.NextActionStatePass, Executor: workboard.GateRunExecutorHuman, CreatedAt: base.Add(5 * time.Hour)},
				// a quality gate run
				{Gate: "completeness", State: workboard.NextActionStateWarn, Hint: "add examples", Executor: workboard.GateRunExecutorPlatform, CreatedAt: base.Add(3 * time.Hour)},
			},
		},
		AuditEvents: &fakeAuditEvents{byArtifact: map[string][]artifact.Event{
			"art-lead": {{
				ID: "e1", ArtifactID: "art-lead", EventType: artifact.EventPublished,
				Payload:   `{"actor":"dave","actor_kind":"human","note":"v1"}`,
				CreatedAt: base,
			}},
		}},
		ReadinessRuns: &fakeAuditReadiness{byArtifact: map[string][]artifact.ReadinessRun{
			"art-lead": {{
				ID: "r1", ArtifactID: "art-lead", Gate: "spec_repo_drift",
				State: artifact.ReadinessStatePass, Executor: "ide_agent", Hint: "no drift",
				CreatedAt: base.Add(1 * time.Hour),
			}},
		}},
		AuditLifecycle: &fakeAuditLifecycle{byEntity: map[string][]workboard.LifecycleEvent{
			"change_request/cr-1": {{
				ID: "le1", EntityKind: "change_request", EntityID: "cr-1",
				EventType: "change_request.archived", Actor: "erin",
				PayloadJSON: `{"reason":"done"}`, CreatedAt: base.Add(4 * time.Hour),
			}},
		}},
	}

	trail, err := svc.AuditTrail(ctx, ResolveWorkRefInput{Ref: "CR-1"}, false)
	if err != nil {
		t.Fatalf("AuditTrail: %v", err)
	}

	if trail.ChangeRequestKey != "CR-1" || trail.FeatureKey != "FEAT-1" || trail.FeatureName != "Audit" {
		t.Fatalf("header wrong: %+v", trail)
	}

	// Sources: published + readiness + gate:completeness + lifecycle + delivery_review = 5.
	if len(trail.Events) != 5 {
		t.Fatalf("got %d events, want 5: %+v", len(trail.Events), trail.Events)
	}

	// Ascending by timestamp.
	for i := 1; i < len(trail.Events); i++ {
		if trail.Events[i-1].Timestamp > trail.Events[i].Timestamp {
			t.Fatalf("events not ascending at %d: %q then %q", i, trail.Events[i-1].Timestamp, trail.Events[i].Timestamp)
		}
	}

	find := func(action string) (AuditEvent, bool) {
		for _, e := range trail.Events {
			if e.Action == action {
				return e, true
			}
		}
		return AuditEvent{}, false
	}

	pub, ok := find("published")
	if !ok || pub.Actor != "dave" || pub.ActorKind != "human" || pub.Trust != "" || pub.Subject != "art-lead" {
		t.Fatalf("published event wrong: %+v (ok=%v)", pub, ok)
	}

	drift, ok := find("gate:spec_repo_drift")
	if !ok || drift.Verdict != "pass" || drift.Trust != "agent_attested" || drift.ActorKind != "agent" {
		t.Fatalf("readiness event wrong (want agent_attested): %+v (ok=%v)", drift, ok)
	}

	dr, ok := find("delivery_review")
	if !ok || dr.Verdict != "pass" || dr.Actor != "carol" || dr.Trust != "human" || dr.Detail != "looks good" {
		t.Fatalf("delivery_review event wrong: %+v (ok=%v)", dr, ok)
	}
	// The latest delivery_review gate run must be deduped in favor of the snapshot.
	count := 0
	for _, e := range trail.Events {
		if e.Action == "delivery_review" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 delivery_review event, got %d", count)
	}
}

func TestAuditTrail_UnresolvableRefReturnsNotFound(t *testing.T) {
	t.Parallel()
	svc := &Service{WorkBoard: &fakeAuditWorkBoard{cr: &workboard.ChangeRequest{ID: "cr-9", Key: "CR-9"}}}
	_, err := svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "CR-DOES-NOT-EXIST"}, false)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAuditTrailFailsWhenConfiguredSourceReadFails(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("audit source unavailable")
	newService := func() *Service {
		return &Service{WorkBoard: &fakeAuditWorkBoard{
			cr:      &workboard.ChangeRequest{ID: "cr-source-error", Key: "CR-SOURCE-ERROR", FeatureID: "feat-1", LeadArtifactID: "art-1"},
			feature: &workboard.Feature{ID: "feat-1", Key: "FEAT-1"},
		}}
	}

	tests := map[string]func(*Service){
		"feature": func(svc *Service) {
			svc.WorkBoard.(*fakeAuditWorkBoard).featureErr = wantErr
		},
		"artifact events": func(svc *Service) {
			svc.AuditEvents = &fakeAuditEvents{err: wantErr}
		},
		"artifact readiness": func(svc *Service) {
			svc.ReadinessRuns = &fakeAuditReadiness{err: wantErr}
		},
		"gate runs": func(svc *Service) {
			svc.WorkBoard.(*fakeAuditWorkBoard).gateErr = wantErr
		},
		"lifecycle events": func(svc *Service) {
			svc.AuditLifecycle = &fakeAuditLifecycle{err: wantErr}
		},
	}
	for name, configure := range tests {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := newService()
			configure(svc)
			_, err := svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "cr-source-error"}, false)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestAuditTrailVerifyRequiresArtifactEventReader(t *testing.T) {
	t.Parallel()
	svc := &Service{WorkBoard: &fakeAuditWorkBoard{
		cr: &workboard.ChangeRequest{ID: "cr-verify-reader", Key: "CR-VERIFY-READER", LeadArtifactID: "art-1"},
	}}

	_, err := svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "cr-verify-reader"}, true)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestAuditTrailRejectsCrossWorkspaceFeature(t *testing.T) {
	t.Parallel()
	svc := &Service{WorkBoard: &fakeAuditWorkBoard{
		cr: &workboard.ChangeRequest{
			ID: "cr-workspace", Key: "CR-WORKSPACE", FeatureID: "feat-workspace", WorkspaceID: "ws-a",
		},
		feature: &workboard.Feature{ID: "feat-workspace", Key: "FEAT-WORKSPACE", WorkspaceID: "ws-b"},
	}}

	_, err := svc.AuditTrail(workspace.WithID(context.Background(), "ws-a"), ResolveWorkRefInput{Ref: "cr-workspace"}, false)
	if !errors.Is(err, workboard.ErrNotFound) {
		t.Fatalf("error = %v, want workspace-scoped not found", err)
	}
}

// With Verify set, the trail carries a chain report aggregated across the
// lineage artifacts: any tampered artifact makes the whole trail tampered,
// naming the artifact and first bad event (per spec: audit --verify).
func TestAuditTrail_VerifyReportsTamperedChain(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	cr := &workboard.ChangeRequest{ID: "cr-v", Key: "CR-V", LeadArtifactID: "art-v"}

	good := artifact.Event{ID: "ev-a", ArtifactID: "art-v", EventType: artifact.EventPublished, Payload: `{}`, CreatedAt: base}
	good.Hash = artifact.EventHash("", good)
	bad := artifact.Event{ID: "ev-b", ArtifactID: "art-v", EventType: artifact.EventSuperseded, Payload: `{}`, CreatedAt: base.Add(time.Minute)}
	bad.PrevHash = good.Hash
	bad.Hash = artifact.EventHash(bad.PrevHash, bad)
	bad.Payload = `{"forged":true}` // simulate direct UPDATE after insert

	svc := &Service{
		WorkBoard:   &fakeAuditWorkBoard{cr: cr},
		AuditEvents: &fakeAuditEvents{byArtifact: map[string][]artifact.Event{"art-v": {good, bad}}},
	}
	trail, err := svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "cr-v"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if trail.Chain == nil {
		t.Fatal("Chain must be set when verify requested")
	}
	if trail.Chain.State != artifact.ChainTampered || trail.Chain.FirstBadEventID != "ev-b" || trail.Chain.ArtifactID != "art-v" {
		t.Fatalf("chain = %+v, want tampered at ev-b on art-v", trail.Chain)
	}

	// Without verify, no chain object (output unchanged).
	trail, err = svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "cr-v"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if trail.Chain != nil {
		t.Fatalf("Chain must be nil without verify, got %+v", trail.Chain)
	}
}

// An intact chain across lineage artifacts verifies as intact.
func TestAuditTrail_VerifyIntact(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	cr := &workboard.ChangeRequest{ID: "cr-i", Key: "CR-I", LeadArtifactID: "art-i"}
	e1 := artifact.Event{ID: "ev-1", ArtifactID: "art-i", EventType: artifact.EventPublished, Payload: `{}`, CreatedAt: base}
	e1.Hash = artifact.EventHash("", e1)
	svc := &Service{
		WorkBoard:   &fakeAuditWorkBoard{cr: cr},
		AuditEvents: &fakeAuditEvents{byArtifact: map[string][]artifact.Event{"art-i": {e1}}},
	}
	trail, err := svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "cr-i"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if trail.Chain == nil || trail.Chain.State != artifact.ChainIntact {
		t.Fatalf("chain = %+v, want intact", trail.Chain)
	}
}

func TestAuditTrailVerifyReportsMissingArtifactEventChainAsTampered(t *testing.T) {
	t.Parallel()
	svc := &Service{
		WorkBoard: &fakeAuditWorkBoard{cr: &workboard.ChangeRequest{
			ID: "cr-empty-chain", Key: "CR-EMPTY-CHAIN", LeadArtifactID: "art-empty-chain",
		}},
		AuditEvents: &fakeAuditEvents{byArtifact: map[string][]artifact.Event{}},
	}

	trail, err := svc.AuditTrail(context.Background(), ResolveWorkRefInput{Ref: "cr-empty-chain"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if trail.Chain == nil || trail.Chain.State != artifact.ChainTampered || trail.Chain.ArtifactID != "art-empty-chain" {
		t.Fatalf("chain = %+v, want missing chain reported as tampered", trail.Chain)
	}
}
