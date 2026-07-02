package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/workboard"
)

// stubStatusReader implements governanceops.WorkBoardReader for status tests.
type stubStatusReader struct {
	crs      []workboard.ChangeRequest
	warnings []workboard.StaleWarning
}

func (s *stubStatusReader) ListChangeRequests(_ context.Context, _ bool) ([]workboard.ChangeRequest, error) {
	return s.crs, nil
}

func (s *stubStatusReader) GetChangeRequest(_ context.Context, _ string) (*workboard.ChangeRequest, error) {
	return nil, workboard.ErrNotFound
}

func (s *stubStatusReader) GetFeature(_ context.Context, _ string) (*workboard.Feature, error) {
	return nil, workboard.ErrNotFound
}

func (s *stubStatusReader) ListAcceptanceCriteria(_ context.Context, _ string) ([]workboard.AcceptanceCriterion, error) {
	return nil, nil
}

func (s *stubStatusReader) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return nil, nil
}

func (s *stubStatusReader) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return s.warnings, nil
}

func callGetGovernanceStatus(t *testing.T, r governanceops.WorkBoardReader) governanceops.GovernanceStatusResult {
	t.Helper()
	svc := &governanceops.Service{WorkBoard: r}
	h := NewGetGovernanceStatusHandler(svc)
	raw, err := h(context.Background(), GovernanceStatusInput{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var out governanceops.GovernanceStatusResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestGetGovernanceStatus_EmptyBoard(t *testing.T) {
	t.Parallel()
	out := callGetGovernanceStatus(t, &stubStatusReader{})
	if out.Counts.Total != 0 {
		t.Errorf("total = %d, want 0", out.Counts.Total)
	}
	if len(out.Attention) != 0 {
		t.Errorf("attention = %v, want empty", out.Attention)
	}
	if out.Summary != "No active work items." {
		t.Errorf("summary = %q, want no-items message", out.Summary)
	}
}

func TestGetGovernanceStatus_PhaseCounts(t *testing.T) {
	t.Parallel()
	r := &stubStatusReader{
		crs: []workboard.ChangeRequest{
			{ID: "cr-1", Key: "CR-1", Title: "A", Phase: workboard.BoardPhaseIntake},
			{ID: "cr-2", Key: "CR-2", Title: "B", Phase: workboard.BoardPhaseReady},
			{ID: "cr-3", Key: "CR-3", Title: "C", Phase: workboard.BoardPhaseHandoff},
			{ID: "cr-4", Key: "CR-4", Title: "D", Phase: workboard.BoardPhaseHandoff},
			{ID: "cr-5", Key: "CR-5", Title: "E", Phase: workboard.BoardPhaseDraft},
		},
	}
	out := callGetGovernanceStatus(t, r)
	if out.Counts.Total != 5 {
		t.Errorf("total = %d, want 5", out.Counts.Total)
	}
	if out.Counts.Intake != 1 {
		t.Errorf("intake = %d, want 1", out.Counts.Intake)
	}
	if out.Counts.Draft != 1 {
		t.Errorf("draft = %d, want 1", out.Counts.Draft)
	}
	if out.Counts.Ready != 1 {
		t.Errorf("ready = %d, want 1", out.Counts.Ready)
	}
	if out.Counts.Handoff != 2 {
		t.Errorf("handoff = %d, want 2", out.Counts.Handoff)
	}
}

func TestGetGovernanceStatus_AttentionFromWarnings(t *testing.T) {
	t.Parallel()
	r := &stubStatusReader{
		crs: []workboard.ChangeRequest{
			{ID: "cr-1", Key: "CR-1", Title: "Fix auth", Phase: workboard.BoardPhaseHandoff},
			{ID: "cr-2", Key: "CR-2", Title: "New feature", Phase: workboard.BoardPhaseReady},
		},
		warnings: []workboard.StaleWarning{
			{Code: workboard.WarningContextPackStale, ChangeRequestID: "cr-1", Severity: "warn", Message: "stale"},
			{Code: workboard.WarningTrackerStatusConflict, ChangeRequestID: "cr-1", Severity: "warn", Message: "conflict"},
		},
	}
	out := callGetGovernanceStatus(t, r)
	if len(out.Attention) != 1 {
		t.Fatalf("attention len = %d, want 1", len(out.Attention))
	}
	item := out.Attention[0]
	if item.Key != "CR-1" {
		t.Errorf("key = %q, want CR-1", item.Key)
	}
	if len(item.Issues) != 2 {
		t.Errorf("issues len = %d, want 2", len(item.Issues))
	}
}

func TestGetGovernanceStatus_CleanBoardNoAttention(t *testing.T) {
	t.Parallel()
	r := &stubStatusReader{
		crs: []workboard.ChangeRequest{
			{ID: "cr-1", Key: "CR-1", Title: "A", Phase: workboard.BoardPhaseReady},
		},
		// no warnings
	}
	out := callGetGovernanceStatus(t, r)
	if len(out.Attention) != 0 {
		t.Errorf("attention = %v, want empty for clean board", out.Attention)
	}
}

func TestGetGovernanceStatus_SummaryContainsPhases(t *testing.T) {
	t.Parallel()
	r := &stubStatusReader{
		crs: []workboard.ChangeRequest{
			{ID: "cr-1", Key: "CR-1", Title: "A", Phase: workboard.BoardPhaseReady},
			{ID: "cr-2", Key: "CR-2", Title: "B", Phase: workboard.BoardPhaseHandoff},
		},
		warnings: []workboard.StaleWarning{
			{Code: workboard.WarningContextPackStale, ChangeRequestID: "cr-2", Severity: "warn"},
		},
	}
	out := callGetGovernanceStatus(t, r)
	if !contains(out.Summary, "2 active") {
		t.Errorf("summary %q missing total count", out.Summary)
	}
	if !contains(out.Summary, "1 ready") {
		t.Errorf("summary %q missing ready count", out.Summary)
	}
	if !contains(out.Summary, "attention") {
		t.Errorf("summary %q missing attention signal", out.Summary)
	}
}

func TestGetGovernanceStatus_DerivesPhaseWhenEmpty(t *testing.T) {
	t.Parallel()
	// Phase field left empty — DerivePhase should be called as fallback.
	r := &stubStatusReader{
		crs: []workboard.ChangeRequest{
			// no Phase set, no artifact IDs → DerivePhase → Intake
			{ID: "cr-1", Key: "CR-1", Title: "A"},
			// has lead artifact → DerivePhase → Ready
			{ID: "cr-2", Key: "CR-2", Title: "B", LeadArtifactID: "art-1"},
		},
	}
	out := callGetGovernanceStatus(t, r)
	if out.Counts.Intake != 1 {
		t.Errorf("intake = %d, want 1", out.Counts.Intake)
	}
	if out.Counts.Ready != 1 {
		t.Errorf("ready = %d, want 1", out.Counts.Ready)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
