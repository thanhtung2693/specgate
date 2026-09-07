package command

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

type coverageFeature struct {
	ID                  string
	Key                 string
	Name                string
	CanonicalArtifactID string
}

type coverageArtifact struct {
	ID             string
	FeatureID      string
	FeatureKey     string
	Version        string
	SourceCriteria []sourceCriterion
}

type sourceCriterion struct {
	ID             string
	DeferredReason string
}

type coverageWork struct {
	Key                string `json:"key"`
	Title              string `json:"title"`
	Phase              string `json:"phase"`
	ArtifactID         string `json:"artifact_id"`
	Current            bool   `json:"current_spec"`
	AcceptanceCriteria []string
}

type specificationCoverage struct {
	FeatureKey     string         `json:"feature_key"`
	FeatureName    string         `json:"feature_name,omitempty"`
	ArtifactID     string         `json:"artifact_id"`
	Version        string         `json:"version"`
	State          string         `json:"state"`
	SourceCoverage string         `json:"source_coverage"`
	WorkItems      []coverageWork `json:"work_items"`
	NextAction     string         `json:"next_action,omitempty"`
}

type workspaceCoverage struct {
	Mode           config.Mode             `json:"mode"`
	WorkspaceID    string                  `json:"workspace_id"`
	Workspace      string                  `json:"workspace,omitempty"`
	Counts         map[string]int          `json:"counts"`
	Specifications []specificationCoverage `json:"specifications"`
}

func newCoverageCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "coverage",
		Short: "Show delivery coverage for every canonical specification",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				result workspaceCoverage
				err    error
			)
			if deps.Topology == config.ModeLocal {
				result, err = localWorkspaceCoverage(cmd, deps)
				if err != nil {
					return localExitError(deps, "coverage", err)
				}
			} else {
				result, err = fullWorkspaceCoverage(cmd, deps)
				if err != nil {
					return apiExitError(deps, "coverage", err)
				}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("coverage", result)
				return nil
			}
			printWorkspaceCoverage(deps, result)
			return nil
		},
	}
}

func localWorkspaceCoverage(cmd *cobra.Command, deps *Deps) (workspaceCoverage, error) {
	store, err := openLocalStore(deps)
	if err != nil {
		return workspaceCoverage{}, err
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return workspaceCoverage{}, err
	}
	localFeatures, err := store.ListFeatures(cmd.Context(), selection.Workspace.ID)
	if err != nil {
		return workspaceCoverage{}, err
	}
	localArtifacts, err := store.ListArtifacts(cmd.Context(), selection.Workspace.ID)
	if err != nil {
		return workspaceCoverage{}, err
	}
	localWork, err := store.ListWork(cmd.Context(), selection.Workspace.ID)
	if err != nil {
		return workspaceCoverage{}, err
	}
	features := make([]coverageFeature, 0, len(localFeatures))
	for _, feature := range localFeatures {
		features = append(features, coverageFeature{
			ID: feature.ID, Key: feature.Key, Name: feature.Key, CanonicalArtifactID: feature.CanonicalArtifactID,
		})
	}
	artifacts := make([]coverageArtifact, 0, len(localArtifacts))
	for _, artifact := range localArtifacts {
		artifacts = append(artifacts, coverageArtifact{
			ID: artifact.ID, FeatureKey: artifact.FeatureKey, Version: "v" + strconv.Itoa(artifact.Version), SourceCriteria: sourceCriterionIDs(artifact.SourceCriteria),
		})
	}
	work := make([]coverageWork, 0, len(localWork))
	for _, item := range localWork {
		work = append(work, coverageWork{Key: item.Key, Title: item.Title, Phase: item.Phase, ArtifactID: item.ArtifactID, AcceptanceCriteria: item.AcceptanceCriteria})
	}
	return buildWorkspaceCoverage(config.ModeLocal, selection.Workspace.ID, selection.Workspace.Slug, features, artifacts, work), nil
}

func fullWorkspaceCoverage(cmd *cobra.Command, deps *Deps) (workspaceCoverage, error) {
	workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
	if err != nil {
		return workspaceCoverage{}, err
	}
	fullFeatures, err := listFeaturesForWorkspace(cmd.Context(), deps, workspaceID, "")
	if err != nil {
		return workspaceCoverage{}, err
	}
	fullArtifacts, err := listAllArtifacts(cmd.Context(), deps, client.ArtifactFilter{WorkspaceID: workspaceID})
	if err != nil {
		return workspaceCoverage{}, err
	}
	fullWork, err := deps.Client.ListWorkItemsIncludingArchived(cmd.Context(), workspaceID)
	if err != nil {
		return workspaceCoverage{}, err
	}
	features := make([]coverageFeature, 0, len(fullFeatures))
	for _, feature := range fullFeatures {
		if strings.TrimSpace(feature.CanonicalArtifactID) == "" {
			continue
		}
		features = append(features, coverageFeature{
			ID: feature.ID, Key: feature.Key, Name: feature.Name, CanonicalArtifactID: feature.CanonicalArtifactID,
		})
	}
	artifacts := make([]coverageArtifact, 0, len(fullArtifacts))
	for _, artifact := range fullArtifacts {
		artifacts = append(artifacts, coverageArtifact{
			ID: artifact.ID, FeatureID: artifact.FeatureID, Version: artifact.Version,
		})
	}
	work := make([]coverageWork, 0, len(fullWork))
	for _, item := range fullWork {
		work = append(work, coverageWork{Key: item.Key, Title: item.Title, Phase: item.Phase, ArtifactID: item.LeadArtifactID})
	}
	selection := currentWorkspaceSelection(deps)
	return buildWorkspaceCoverage(config.ModeFull, workspaceID, selection.Workspace.Slug, features, artifacts, work), nil
}

func listAllArtifacts(ctx context.Context, deps *Deps, filter client.ArtifactFilter) ([]client.Artifact, error) {
	const pageSize = 200
	filter.Limit = pageSize
	filter.Offset = 0
	result := make([]client.Artifact, 0)
	for {
		page, err := deps.Client.ListArtifacts(ctx, filter)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if len(result) >= page.Total || len(page.Items) == 0 {
			return result, nil
		}
		filter.Offset += len(page.Items)
	}
}

func buildWorkspaceCoverage(mode config.Mode, workspaceID, workspace string, features []coverageFeature, artifacts []coverageArtifact, work []coverageWork) workspaceCoverage {
	artifactFeature := make(map[string]string, len(artifacts))
	artifactVersion := make(map[string]string, len(artifacts))
	artifactCriteria := make(map[string][]sourceCriterion, len(artifacts))
	for _, artifact := range artifacts {
		key := artifact.FeatureID
		if key == "" {
			key = artifact.FeatureKey
		}
		artifactFeature[artifact.ID] = key
		artifactVersion[artifact.ID] = artifact.Version
		artifactCriteria[artifact.ID] = artifact.SourceCriteria
	}
	sort.Slice(features, func(i, j int) bool { return features[i].Key < features[j].Key })
	result := workspaceCoverage{
		Mode: mode, WorkspaceID: workspaceID, Workspace: workspace,
		Counts:         map[string]int{"uncovered": 0, "unfinished": 0, "stale": 0, "delivered": 0},
		Specifications: make([]specificationCoverage, 0, len(features)),
	}
	for _, feature := range features {
		row := specificationCoverage{
			FeatureKey: feature.Key, FeatureName: feature.Name, ArtifactID: feature.CanonicalArtifactID, Version: artifactVersion[feature.CanonicalArtifactID], SourceCoverage: "unknown", WorkItems: []coverageWork{},
		}
		currentDelivered := false
		currentUnfinished := false
		staleDelivered := false
		for _, item := range work {
			if artifactFeature[item.ArtifactID] != feature.ID && artifactFeature[item.ArtifactID] != feature.Key {
				continue
			}
			item.Current = item.ArtifactID == feature.CanonicalArtifactID
			row.WorkItems = append(row.WorkItems, item)
			switch {
			case item.Current && isDeliveredPhase(item.Phase):
				currentDelivered = true
			case item.Current:
				currentUnfinished = true
			case isDeliveredPhase(item.Phase):
				staleDelivered = true
			}
		}
		switch {
		case currentUnfinished:
			row.State = "unfinished"
			row.NextAction = "specgate verify " + firstUnfinishedCurrentWorkRef(row.WorkItems)
		case currentDelivered:
			row.State = "delivered"
		case staleDelivered:
			row.State = "stale"
			row.NextAction = createCoverageWorkCommand(feature)
		default:
			row.State = "uncovered"
			row.NextAction = createCoverageWorkCommand(feature)
		}
		row.SourceCoverage = sourceCoverage(artifactCriteria[feature.CanonicalArtifactID], row.WorkItems)
		result.Counts[row.State]++
		result.Specifications = append(result.Specifications, row)
	}
	return result
}

func firstUnfinishedCurrentWorkRef(items []coverageWork) string {
	for _, item := range items {
		if item.Current && !isDeliveredPhase(item.Phase) {
			return item.Key
		}
	}
	return ""
}

func isDeliveredPhase(phase string) bool {
	return strings.EqualFold(strings.TrimSpace(phase), "delivered")
}

func sourceCriterionIDs(criteria []local.SourceCriterion) []sourceCriterion {
	ids := make([]sourceCriterion, 0, len(criteria))
	for _, criterion := range criteria {
		ids = append(ids, sourceCriterion{ID: criterion.ID, DeferredReason: criterion.DeferredReason})
	}
	return ids
}

func sourceCoverage(criteria []sourceCriterion, work []coverageWork) string {
	if len(criteria) == 0 {
		return "unknown"
	}
	deferred := false
	accountedFor := false
	for _, criterion := range criteria {
		if criterion.DeferredReason != "" {
			deferred = true
			continue
		}
		id := criterion.ID
		mapped := false
		delivered := true
		for _, item := range work {
			if !item.Current {
				continue
			}
			for _, acceptance := range item.AcceptanceCriteria {
				if hasSourceCriterion(acceptance, id) {
					mapped = true
					delivered = delivered && isDeliveredPhase(item.Phase)
				}
			}
		}
		if !mapped {
			return "unassigned"
		}
		if !delivered {
			accountedFor = true
		}
	}
	if deferred || accountedFor {
		return "accounted_for"
	}
	return "delivered"
}

func hasSourceCriterion(acceptance, id string) bool {
	want := "@source:" + id
	for _, token := range strings.Fields(acceptance) {
		if strings.Trim(token, ".,;:!?)]}\"'") == want {
			return true
		}
	}
	return false
}

func createCoverageWorkCommand(feature coverageFeature) string {
	return fmt.Sprintf("specgate artifact show %s --json", feature.CanonicalArtifactID)
}

func printWorkspaceCoverage(deps *Deps, result workspaceCoverage) {
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace:"), result.Workspace)
	fmt.Fprintf(deps.Stdout, "%s %d delivered, %d unfinished, %d stale, %d uncovered\n",
		label(deps, "Coverage:"),
		result.Counts["delivered"], result.Counts["unfinished"], result.Counts["stale"], result.Counts["uncovered"])
	for _, spec := range result.Specifications {
		fmt.Fprintf(deps.Stdout, "%s %s %s — %s\n", styled(deps, output.StyleBold, spec.FeatureKey), spec.Version, spec.ArtifactID, styledStatus(deps, spec.State))
		fmt.Fprintf(deps.Stdout, "  Source requirements: %s\n", styledStatus(deps, spec.SourceCoverage))
		for _, item := range spec.WorkItems {
			fmt.Fprintf(deps.Stdout, "  %s [%s] %s\n", item.Key, item.Phase, item.ArtifactID)
		}
		if spec.NextAction != "" {
			fmt.Fprintf(deps.Stdout, "  Next: %s\n", spec.NextAction)
		}
	}
}
