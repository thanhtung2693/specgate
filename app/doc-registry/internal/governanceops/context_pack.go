package governanceops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
)

const (
	maxContextPackChars     = 96 * 1024
	contextTruncationMarker = "\n\n<!-- specgate: context pack truncated to stay within the model-input budget -->\n\n"
)

// ContextPack assembles the on-read handoff pack for a change request or
// artifact. Kind must be "change_request" or "artifact".
func (s *Service) ContextPack(ctx context.Context, in ContextPackInput) (ContextPackResult, error) {
	if in.Kind == "artifact" {
		return s.contextPackForArtifact(ctx, in.ID)
	}
	return s.contextPackForCR(ctx, in.ID)
}

func (s *Service) contextPackForArtifact(ctx context.Context, artifactID string) (ContextPackResult, error) {
	if s.Artifacts == nil {
		return ContextPackResult{}, fmt.Errorf("%w: artifact reader is not configured", ErrUnavailable)
	}
	art, err := s.Artifacts.Get(ctx, artifactID)
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			return ContextPackResult{}, workboard.ErrNotFound
		}
		return ContextPackResult{}, err
	}
	if selected := trustedWorkspace(ctx); selected != "" && strings.TrimSpace(art.WorkspaceID) != selected {
		return ContextPackResult{}, workboard.ErrNotFound
	}
	ctx = skills.WithWorkspace(ctx, art.WorkspaceID)
	profile, err := parseContextPackProfile(art)
	if err != nil {
		return ContextPackResult{}, err
	}
	markdown, err := renderContextPackMarkdown(ctx, s.Artifacts, nil, s.Skills, art, profile, nil, nil, nil, "", "")
	if err != nil {
		return ContextPackResult{}, err
	}
	markdown = capContextPack(markdown)

	return ContextPackResult{
		Kind:                "artifact",
		ArtifactID:          art.ID,
		State:               "assembled",
		Markdown:            markdown,
		KnowledgeProvenance: []ProvenanceRow{},
		Warnings:            []Warning{},
		GovernanceLevel:     string(profile.GovernanceLevel),
	}, nil
}

func (s *Service) contextPackForCR(ctx context.Context, changeRequestID string) (ContextPackResult, error) {
	if s.WorkBoard == nil {
		return ContextPackResult{}, ErrUnavailable
	}
	cr, err := s.WorkBoard.GetChangeRequest(ctx, changeRequestID)
	if err != nil {
		return ContextPackResult{}, err
	}
	if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
		return ContextPackResult{}, err
	}
	ctx = skills.WithWorkspace(ctx, cr.WorkspaceID)
	var feature *workboard.Feature
	featureID := strings.TrimSpace(cr.FeatureID)
	if featureID != "" {
		loaded, err := s.WorkBoard.GetFeature(ctx, featureID)
		if err != nil {
			return ContextPackResult{}, err
		}
		if err := requireFeatureWorkspace(ctx, loaded); err != nil {
			return ContextPackResult{}, err
		}
		feature = loaded
	}
	rawWarnings, err := s.WorkBoard.ListStaleWarnings(ctx, workboard.StaleWarningFilter{
		ChangeRequestID: changeRequestID,
	})
	if err != nil {
		return ContextPackResult{}, err
	}
	warnings := make([]Warning, 0, len(rawWarnings))
	for _, w := range rawWarnings {
		warnings = append(warnings, Warning{
			Code:       string(w.Code),
			Message:    w.Message,
			ArtifactID: w.ArtifactID,
		})
	}
	featureRefs := []string{}
	if featureID != "" {
		featureRefs = append(featureRefs, featureID)
	}
	if feature != nil && strings.TrimSpace(feature.Key) != "" && feature.Key != featureID {
		featureRefs = append(featureRefs, strings.TrimSpace(feature.Key))
	}
	provenance, provWarnings := buildKnowledgeProvenance(ctx, s.Knowledge, cr.WorkspaceID, featureRefs, cr.ID)
	warnings = append(warnings, provWarnings...)

	outstanding := ""
	unresolvedGates := ""
	runs, runErr := s.WorkBoard.ListGateRuns(ctx, changeRequestID, 50)
	if current, ok := s.WorkBoard.(interface {
		CurrentGateRuns(context.Context, string) ([]workboard.GateRun, error)
	}); ok {
		runs, runErr = current.CurrentGateRuns(ctx, changeRequestID)
	}
	if runErr != nil {
		return ContextPackResult{}, fmt.Errorf("%w: gate state: %v", ErrUnavailable, runErr)
	}
	var authoritative *workboard.GateRun
	authoritative, authoritativeErr := authoritativeDeliveryReviewRun(
		ctx,
		s.WorkBoard,
		changeRequestID,
	)
	if authoritativeErr != nil {
		return ContextPackResult{}, fmt.Errorf("%w: delivery review state: %v", ErrUnavailable, authoritativeErr)
	}
	reviewOutdated := false
	completion, completionErr := s.latestCompletionRecord(ctx, changeRequestID)
	if completionErr != nil {
		return ContextPackResult{}, fmt.Errorf("%w: completion state: %v", ErrUnavailable, completionErr)
	}
	if completion != nil {
		evidenceJSON := ""
		if authoritative != nil {
			evidenceJSON = authoritative.EvidenceJSON
		}
		wrapper, _ := decodeDeliveryReview(evidenceJSON)
		reviewOutdated = authoritative == nil ||
			strings.TrimSpace(wrapper.CompletionFeedbackEventID) != completion.Event.ID
	}
	if reviewOutdated {
		filtered := make([]workboard.GateRun, 0, len(runs))
		for i := range runs {
			if runs[i].Gate != "delivery_review" {
				filtered = append(filtered, runs[i])
			}
		}
		runs = filtered
		authoritative = nil
	}
	if authoritative != nil {
		outstanding = outstandingReviewFeedback([]workboard.GateRun{*authoritative})
		filtered := make([]workboard.GateRun, 0, len(runs)+1)
		for i := range runs {
			if runs[i].Gate != "delivery_review" {
				filtered = append(filtered, runs[i])
			}
		}
		runs = append(filtered, *authoritative)
	}
	if authoritative == nil && !reviewOutdated {
		outstanding = outstandingReviewFeedback(runs)
	}
	unresolvedGates = unresolvedQualityGates(runs)

	renderCR := *cr
	sourceArtifactID := cr.LeadArtifactID
	if strings.TrimSpace(sourceArtifactID) == "" && feature != nil {
		sourceArtifactID = feature.CanonicalArtifactID
	}
	var sourceArtifact *artifact.Artifact
	if id := strings.TrimSpace(sourceArtifactID); id != "" {
		if s.Artifacts == nil {
			return ContextPackResult{}, fmt.Errorf("%w: source artifact reader is not configured", ErrUnavailable)
		}
		art, artErr := s.Artifacts.Get(ctx, id)
		if artErr != nil {
			return ContextPackResult{}, fmt.Errorf("%w: source artifact %q: %v", ErrUnavailable, id, artErr)
		}
		if strings.TrimSpace(art.WorkspaceID) != strings.TrimSpace(cr.WorkspaceID) {
			return ContextPackResult{}, workboard.ErrNotFound
		}
		sourceArtifact = art
	}
	assemble := sourceArtifact != nil ||
		(strings.TrimSpace(sourceArtifactID) == "" && cr.WorkType == workboard.WorkTypeBugFix)
	if assemble {
		rows, err := s.WorkBoard.ListAcceptanceCriteria(ctx, changeRequestID)
		if err != nil {
			return ContextPackResult{}, err
		}
		items := make([]string, 0, len(rows))
		for _, row := range rows {
			if text := strings.TrimSpace(row.Text); text != "" {
				items = append(items, text)
			}
		}
		if len(items) == 0 {
			return ContextPackResult{}, fmt.Errorf("%w: canonical acceptance criteria are unavailable", ErrValidation)
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			return ContextPackResult{}, err
		}
		renderCR.AcceptanceCriteria = string(encoded)
	}
	// spec_repo_drift is an artifact-scoped readiness run, never a CR gate_run,
	// so ListGateRuns above cannot see it. Pull it from the source artifact's
	// readiness runs and merge its findings into Unresolved Quality Gates, or the
	// drift warn is silently dropped from the full-route handoff (per agents spec §6).
	if s.ReadinessRuns != nil {
		if id := strings.TrimSpace(sourceArtifactID); id != "" {
			rruns, rErr := s.ReadinessRuns.ListReadinessRuns(ctx, id, 50)
			if rErr != nil {
				return ContextPackResult{}, fmt.Errorf("%w: readiness state: %v", ErrUnavailable, rErr)
			}
			unresolvedGates = mergeDriftReadiness(unresolvedGates, rruns)
		}
	}
	state := "not_generated"
	markdown := ""
	governanceLevel := ""
	if sourceArtifact != nil {
		profile, profileErr := parseContextPackProfile(sourceArtifact)
		if profileErr != nil {
			return ContextPackResult{}, profileErr
		}
		state = "assembled"
		governanceLevel = string(profile.GovernanceLevel)
		if markdown == "" {
			markdown, err = renderContextPackMarkdown(ctx, s.Artifacts, s.Attachments, s.Skills, sourceArtifact, profile, &renderCR, feature, provenance, outstanding, unresolvedGates)
			if err != nil {
				return ContextPackResult{}, err
			}
			markdown = capContextPack(markdown)
		}
	} else if assemble {
		state = "assembled"
		markdown = capContextPack(renderQuickContextPack(&renderCR, feature, provenance, outstanding, unresolvedGates))
	}

	return ContextPackResult{
		ChangeRequestID:     cr.ID,
		FeatureID:           featureID,
		SourceArtifactID:    sourceArtifactID,
		State:               state,
		Markdown:            markdown,
		KnowledgeProvenance: provenance,
		Warnings:            warnings,
		GovernanceLevel:     governanceLevel,
	}, nil
}

func capContextPack(markdown string) string {
	if len(markdown) <= maxContextPackChars {
		return markdown
	}
	available := maxContextPackChars - len(contextTruncationMarker)
	if available <= 0 {
		return contextTruncationMarker
	}
	headBytes := available * 3 / 4
	tailBytes := available - headBytes
	headBytes = utf8PrefixBoundary(markdown, headBytes)
	tailStart := utf8SuffixBoundary(markdown, len(markdown)-tailBytes)
	return markdown[:headBytes] + contextTruncationMarker + markdown[tailStart:]
}

func utf8PrefixBoundary(value string, end int) int {
	if end >= len(value) {
		return len(value)
	}
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return end
}

func utf8SuffixBoundary(value string, start int) int {
	if start <= 0 {
		return 0
	}
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return start
}
