package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workboard"
)

// AuditTrail assembles the full chronological governance history for a work
// reference — the "git log for governance". It resolves ref to a change request
// (and its feature), reads every read-only governance source, maps each row to
// an AuditEvent, and returns them sorted by timestamp ascending.
//
// Nil optional readers contribute no rows. Once a reader is configured, a read
// failure aborts the request so a partial trail cannot look complete. Resolution
// failure (unknown ref) returns ErrNotFound so the HTTP layer maps it to 404.
//
// When verify is true the lineage artifacts' event chains are recomputed and
// the aggregated tamper-evidence report is attached as Chain (spec §8): any
// tampered artifact makes the trail tampered.
func (s *Service) AuditTrail(ctx context.Context, in ResolveWorkRefInput, verify bool) (AuditTrail, error) {
	resolved, err := s.ResolveWorkRef(ctx, in)
	if err != nil {
		return AuditTrail{}, err
	}
	cr, err := s.WorkBoard.GetChangeRequest(ctx, resolved.ChangeRequestID)
	if err != nil {
		return AuditTrail{}, err
	}

	trail := AuditTrail{
		Ref:              strings.TrimSpace(in.Ref),
		ChangeRequestID:  cr.ID,
		ChangeRequestKey: cr.Key,
		FeatureID:        cr.FeatureID,
		Title:            cr.Title,
		Phase:            resolved.Phase,
		Events:           []AuditEvent{},
	}

	var feature *workboard.Feature
	if strings.TrimSpace(cr.FeatureID) != "" {
		f, featureErr := s.WorkBoard.GetFeature(ctx, cr.FeatureID)
		if featureErr != nil {
			return AuditTrail{}, fmt.Errorf("%w: feature audit scope: %v", ErrUnavailable, featureErr)
		}
		if f != nil {
			if err := requireFeatureWorkspace(ctx, f); err != nil {
				return AuditTrail{}, err
			}
			feature = f
			trail.FeatureKey = f.Key
			trail.FeatureName = f.Name
		}
	}

	// Collect (event, sort-key) pairs across all sources, then order once.
	type dated struct {
		ev AuditEvent
		ts time.Time
	}
	var acc []dated
	add := func(ts time.Time, ev AuditEvent) {
		ev.Timestamp = ts.UTC().Format(time.RFC3339)
		acc = append(acc, dated{ev: ev, ts: ts})
	}

	// Feature lineage: lead + canonical artifacts. Dedupe so a
	// single artifact playing two roles is not read twice.
	artifactIDs := dedupeNonEmpty(cr.LeadArtifactID, featureCanonicalID(feature))

	// Source 1 + 2: artifact status events and readiness runs, per artifact.
	var chain *artifact.ChainReport
	if verify && len(artifactIDs) > 0 && s.AuditEvents == nil {
		return AuditTrail{}, fmt.Errorf("%w: artifact event reader is not configured", ErrUnavailable)
	}
	for _, artifactID := range artifactIDs {
		if s.AuditEvents != nil {
			events, evErr := s.AuditEvents.ListEvents(ctx, artifact.EventFilter{ArtifactID: artifactID, Limit: -1})
			if evErr != nil {
				return AuditTrail{}, fmt.Errorf("%w: artifact events for %q: %v", ErrUnavailable, artifactID, evErr)
			}
			if verify {
				report := artifact.ChainReport{State: artifact.ChainTampered, ArtifactID: artifactID}
				if len(events) > 0 {
					report = artifact.VerifyEventChain(events)
				}
				chain = worseChain(chain, &report)
			}
			for _, ev := range events {
				p := parseStatusEventPayload(ev.Payload)
				add(ev.CreatedAt, AuditEvent{
					Actor:     p.Actor,
					ActorKind: p.ActorKind,
					Action:    artifactAuditAction(ev),
					Subject:   artifactID,
					Detail:    p.Note,
				})
			}
		}
		if s.ReadinessRuns != nil {
			runs, rErr := s.ReadinessRuns.ListReadinessRuns(ctx, artifactID, -1)
			if rErr != nil {
				return AuditTrail{}, fmt.Errorf("%w: artifact readiness for %q: %v", ErrUnavailable, artifactID, rErr)
			}
			for _, run := range runs {
				add(run.CreatedAt, AuditEvent{
					Actor:     run.Executor,
					ActorKind: deriveActorKind(run.Executor),
					Action:    "gate:" + run.Gate,
					Subject:   artifactID,
					Verdict:   string(run.State),
					Trust:     deriveTrust(run.Executor),
					Detail:    run.Hint,
				})
			}
		}
	}

	// Source 3: CR gate runs. delivery_review runs are folded from the richer
	// snapshot below (source 5), except older non-latest ones which stay as
	// history so delivery rework is visible.
	if s.WorkBoard != nil {
		runs, rErr := s.WorkBoard.ListGateRuns(ctx, cr.ID, -1)
		if rErr != nil {
			return AuditTrail{}, fmt.Errorf("%w: change-request gates: %v", ErrUnavailable, rErr)
		}
		for _, run := range runs {
			if run.Gate == governanceprofile.DeliveryReviewGateKey {
				if cr.DeliveryReview != nil && run.CreatedAt.Equal(cr.DeliveryReview.ReviewedAt) {
					continue // owned by source 5 (richer actor/note)
				}
				add(run.CreatedAt, AuditEvent{
					Actor:     run.Executor,
					ActorKind: deriveActorKind(run.Executor),
					Action:    "delivery_review",
					Subject:   cr.Key,
					Verdict:   string(run.State),
					Trust:     deriveTrust(run.Executor),
					Detail:    run.Hint,
				})
				continue
			}
			add(run.CreatedAt, AuditEvent{
				Actor:     run.Executor,
				ActorKind: deriveActorKind(run.Executor),
				Action:    "gate:" + run.Gate,
				Subject:   cr.Key,
				Verdict:   string(run.State),
				Trust:     deriveTrust(run.Executor),
				Detail:    run.Hint,
			})
		}
	}

	// Source 4: workboard lifecycle events for both the feature and the CR.
	if s.AuditLifecycle != nil {
		for _, scope := range []struct{ kind, id, subject string }{
			{"feature", cr.FeatureID, trailFeatureSubject(feature, cr.FeatureID)},
			{"change_request", cr.ID, cr.Key},
		} {
			if strings.TrimSpace(scope.id) == "" {
				continue
			}
			events, lErr := s.AuditLifecycle.ListLifecycleEvents(ctx, scope.kind, scope.id, -1)
			if lErr != nil {
				return AuditTrail{}, fmt.Errorf("%w: %s lifecycle events: %v", ErrUnavailable, scope.kind, lErr)
			}
			for _, ev := range events {
				add(ev.CreatedAt, AuditEvent{
					Actor:   ev.Actor,
					Action:  ev.EventType,
					Subject: scope.subject,
					Detail:  parseLifecycleDetail(ev.PayloadJSON),
				})
			}
		}
	}

	// Source 5: the authoritative (latest) delivery_review snapshot.
	if cr.DeliveryReview != nil {
		dr := cr.DeliveryReview
		add(dr.ReviewedAt, AuditEvent{
			Actor:     dr.Actor,
			ActorKind: deriveActorKind(dr.Executor),
			Action:    "delivery_review",
			Subject:   cr.Key,
			Verdict:   dr.Verdict,
			Trust:     deriveTrust(dr.Executor),
			Detail:    firstNonEmpty(dr.Note, dr.Summary, dr.Hint),
		})
	}

	sort.SliceStable(acc, func(i, j int) bool { return acc[i].ts.Before(acc[j].ts) })
	for _, d := range acc {
		trail.Events = append(trail.Events, d.ev)
	}
	if verify {
		if chain == nil {
			chain = &artifact.ChainReport{State: artifact.ChainIntact}
		}
		trail.Chain = chain
	}
	return trail, nil
}

// worseChain keeps the most severe report across lineage artifacts.
func worseChain(current, candidate *artifact.ChainReport) *artifact.ChainReport {
	if current == nil {
		return candidate
	}
	rank := map[artifact.ChainState]int{
		artifact.ChainIntact:   0,
		artifact.ChainTampered: 1,
	}
	if rank[candidate.State] > rank[current.State] {
		return candidate
	}
	return current
}

// deriveTrust maps a gate-run executor to its trust tier (per doc-registry spec
// §6 audit trail). Empty executor (artifact status / lifecycle events) yields an
// empty trust. Derived at read time — no trust_tier column exists.
func deriveTrust(executor string) string {
	switch executor {
	case workboard.GateRunExecutorHuman:
		return "human"
	case workboard.GateRunExecutorIDEAgent:
		return "agent_attested"
	case workboard.GateRunExecutorPlatform:
		return "platform"
	default:
		return ""
	}
}

// deriveActorKind maps an executor to the human|agent|platform actor kind.
func deriveActorKind(executor string) string {
	switch executor {
	case workboard.GateRunExecutorHuman:
		return "human"
	case workboard.GateRunExecutorIDEAgent:
		return "agent"
	case workboard.GateRunExecutorPlatform:
		return "platform"
	default:
		return ""
	}
}

// statusEventPayload is the actor provenance carried by artifact status events
// (per doc-registry spec §8).
type statusEventPayload struct {
	Actor     string `json:"actor"`
	ActorKind string `json:"actor_kind"`
	Note      string `json:"note"`
}

func parseStatusEventPayload(raw string) statusEventPayload {
	var p statusEventPayload
	if strings.TrimSpace(raw) == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

func artifactAuditAction(event artifact.Event) string {
	return strings.TrimPrefix(event.EventType, "artifact.")
}

// parseLifecycleDetail extracts a short human detail from a lifecycle event
// payload without assuming a fixed shape.
func parseLifecycleDetail(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var p struct {
		Note   string `json:"note"`
		Reason string `json:"reason"`
		From   string `json:"from"`
		To     string `json:"to"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ""
	}
	if detail := firstNonEmpty(p.Note, p.Reason); detail != "" {
		return detail
	}
	if p.From != "" || p.To != "" {
		return strings.TrimSpace(p.From + " → " + p.To)
	}
	return ""
}

func featureCanonicalID(f *workboard.Feature) string {
	if f == nil {
		return ""
	}
	return f.CanonicalArtifactID
}

func trailFeatureSubject(f *workboard.Feature, fallback string) string {
	if f != nil && strings.TrimSpace(f.Key) != "" {
		return f.Key
	}
	return fallback
}

func dedupeNonEmpty(values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
