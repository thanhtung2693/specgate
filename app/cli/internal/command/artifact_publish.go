package command

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func newArtifactPublishCmd(deps *Deps) *cobra.Command {
	var filePath string
	var previewOnly bool
	var compareArtifactID string
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an artifact version from a JSON file",
		Long: `Publish one immutable, path-preserving artifact version from a JSON file.

Use --preview for a zero-write local preview. Add --compare only with --preview
to compare explicit paths, roles, and hashes against one stored artifact.`,
		Example: `  specgate artifact publish --file artifact.json --preview --json
  specgate artifact publish --file artifact.json --preview --compare <artifact-id> --json
  specgate artifact publish --file artifact.json --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if compareArtifactID != "" && !previewOnly {
				err := errors.New("--compare requires --preview")
				payload := output.ErrorPayload{Code: "usage", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if filePath == "" {
				payload := output.ErrorPayload{Code: "usage", Message: "--file is required"}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: ErrInputRequired}
			}
			body, err := readJSONBodyFile(deps, "artifact.publish", filePath)
			if err != nil {
				return err
			}
			if err := normalizeArtifactPublishBody(body); err != nil {
				payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if err := validateArtifactPublishFields(body); err != nil {
				payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			projectRoot, _ := config.FindProjectRoot(deps.WorkingDir)
			documentSources, err := expandArtifactDocumentSources(body, filePath, projectRoot)
			if err != nil {
				payload := output.ErrorPayload{Code: "usage", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if previewOnly {
				preview := artifactPublishPreview(body, documentSources)
				policy, err := artifactPreviewPolicy(cmd.Context(), deps, body)
				if err != nil {
					if deps.Topology == config.ModeLocal {
						return localExitError(deps, "artifact.publish.preview", err)
					}
					return apiExitError(deps, "artifact.publish.preview", err)
				}
				preview["policy"] = policy
				preview["missing_roles"] = missingArtifactRoles(body, policy.RequiredRoles)
				if deps.Topology == config.ModeLocal {
					delete(preview, "governance_hint")
					preview["omitted"] = []string{}
				}
				var comparison *artifactComparison
				if compareArtifactID != "" {
					var base *client.Artifact
					var baseFiles []client.ArtifactFile
					if deps.Topology == config.ModeLocal {
						store, err := openLocalStore(deps)
						if err != nil {
							return localExitError(deps, "artifact.publish.preview", err)
						}
						defer store.Close()
						selection, err := localArtifactSelection(cmd.Context(), deps, store, body)
						if err != nil {
							return localExitError(deps, "artifact.publish.preview", err)
						}
						localBase, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, compareArtifactID)
						if err != nil {
							return localExitError(deps, "artifact.publish.preview", err)
						}
						base, baseFiles = localArtifactComparisonBase(localBase)
					} else {
						previewCtx, err := artifactPublishPreviewContext(cmd.Context(), deps, body)
						if err != nil {
							return apiExitError(deps, "artifact.publish.preview", err)
						}
						base, err = deps.Client.GetArtifact(previewCtx, compareArtifactID)
						if err != nil {
							return apiExitError(deps, "artifact.publish.preview", err)
						}
						baseFiles, err = deps.Client.ListArtifactFiles(previewCtx, compareArtifactID)
						if err != nil {
							return apiExitError(deps, "artifact.publish.preview", err)
						}
					}
					if requestedBase, _ := body["base_version"].(string); requestedBase != "" && requestedBase != base.Version {
						err := fmt.Errorf("base_version %q does not match compared artifact version %q", requestedBase, base.Version)
						payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
						code := deps.Printer.Error("artifact.publish.preview", payload)
						return &output.ExitError{Code: code, Err: err}
					}
					built, err := buildArtifactComparison(body, base, baseFiles)
					if err != nil {
						payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
						code := deps.Printer.Error("artifact.publish.preview", payload)
						return &output.ExitError{Code: code, Err: err}
					}
					comparison = &built
					preview["comparison"] = built
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.publish.preview", preview)
					return nil
				}
				fmt.Fprintln(deps.Stdout, title(deps, "Artifact publish preview:"))
				for _, doc := range preview["documents"].([]map[string]any) {
					fmt.Fprintf(deps.Stdout, "%s\t%s\t%d bytes\n", styled(deps, output.StyleBold, fmt.Sprint(doc["path"])), doc["role"], doc["size_bytes"])
				}
				fmt.Fprintf(deps.Stdout, "Policy\t%s (%s)\n", policy.GovernanceLevel, strings.Join(policy.ReasonCodes, ", "))
				if missing := preview["missing_roles"].([]string); len(missing) > 0 {
					fmt.Fprintf(deps.Stdout, "Missing roles\t%s\n", strings.Join(missing, ", "))
				}
				fmt.Fprintf(deps.Stdout, "Evidence\t%s\n", strings.Join(policy.RequiredEvidence, ", "))
				if comparison != nil {
					writeArtifactComparison(deps.Stdout, *comparison)
				}
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleWarning, "No publication performed", "Human confirmation required before publishing."))
				return nil
			}
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				defer store.Close()
				selection, err := localArtifactSelection(cmd.Context(), deps, store, body)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				input, err := localArtifactInput(body)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				artifact, err := store.PublishArtifact(cmd.Context(), selection.Workspace.ID, input)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				result := map[string]any{"artifact_id": artifact.ID, "version": artifact.Version, "status": artifact.Status, "snapshot_digest": artifact.SnapshotDigest}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.publish", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s (v%d)\n", styled(deps, output.StyleSuccess, "Published"), styled(deps, output.StyleBold, artifact.ID), artifact.Version)
				return nil
			}
			if err := annotateBodyWithCurrentSelection(cmd.Context(), deps, body); err != nil {
				return apiExitError(deps, "artifact.publish", err)
			}
			// Collect impact_declaration interactively when the session is a
			// real TTY and the field is absent from the JSON file. Non-TTY
			// sessions proceed without a declaration (same as --no-input)
			// instead of blocking on a prompt nobody can answer.
			if sessionInteractive(deps) {
				if _, ok := body["impact_declaration"]; !ok {
					answers, err := interactive.CollectImpactDeclaration(deps.Stdin, deps.Stdout, interactive.ImpactAnswers{})
					if err != nil {
						payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("impact declaration: %v", err)}
						code := deps.Printer.Error("artifact.publish", payload)
						return &output.ExitError{Code: code, Err: err}
					}
					body["impact_declaration"] = interactive.NormalizeImpactAnswers(answers)
				}
			}
			result, err := deps.Client.PublishArtifact(requestContextForBody(cmd.Context(), body), body)
			if err != nil {
				return apiExitError(deps, "artifact.publish", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.publish", result)
				return nil
			}
			if id, ok := result["artifact_id"].(string); ok {
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Published"), styled(deps, output.StyleBold, id))
			} else {
				fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Published artifact"))
			}
			// Publish is deliberately non-blocking on required roles (spec-first
			// drafts are legitimate), but a human in plain mode should see the
			// gap now instead of discovering it at gate time.
			if missing, ok := result["missing_roles"].([]any); ok && len(missing) > 0 {
				hint, _ := result["readiness_hint"].(string)
				if hint == "" {
					hint = fmt.Sprintf("missing required roles: %v", missing)
				}
				fmt.Fprintf(deps.Stdout, "%s %s — add the missing documents and republish before readiness gates.\n", styled(deps, output.StyleWarning, "!"), hint)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "JSON file to publish (required)")
	cmd.Flags().BoolVar(&previewOnly, "preview", false, "Show exact package and governance mapping without publishing")
	cmd.Flags().StringVar(&compareArtifactID, "compare", "", "Compare preview with one published artifact using stored hashes")
	return cmd
}

func artifactPreviewPolicy(ctx context.Context, deps *Deps, body map[string]any) (*client.PolicyProjection, error) {
	if deps.Topology == config.ModeLocal {
		policy, err := local.PreviewPolicy()
		if err != nil {
			return nil, err
		}
		rubrics := make([]client.PolicyRubricProjection, len(policy.Rubrics))
		for i, rubric := range policy.Rubrics {
			rubrics[i] = client.PolicyRubricProjection{
				Gate: rubric.Gate, Skill: rubric.Skill, Digest: rubric.Digest, Source: rubric.Source,
			}
		}
		return &client.PolicyProjection{
			GovernanceLevel: policy.GovernanceLevel, ReasonCodes: policy.ReasonCodes,
			RequiredRoles: policy.RequiredRoles, RequiredTopics: policy.RequiredTopics,
			RequiredEvidence: policy.RequiredEvidence, EnabledGates: policy.EnabledGates,
			ApprovalPolicy: policy.ApprovalPolicy, EvidencePolicy: policy.EvidencePolicy,
			PolicyDigest: policy.PolicyDigest, Rubrics: rubrics,
			Explanation: client.PolicyExplanation{
				GovernanceLevel: "standard",
				Title:           "Local Standard governance",
				Summary:         "Human approval and test evidence are required.",
				Reasons:         policy.ReasonCodes,
			},
		}, nil
	}
	previewCtx, err := artifactPublishPreviewContext(ctx, deps, body)
	if err != nil {
		return nil, err
	}
	impactDeclaration, _ := body["impact_declaration"].(map[string]any)
	impactLevel := artifactString(body, "impact_level")
	if impactLevel == "" {
		impactLevel = "medium"
	}
	return deps.Client.ResolveGovernancePolicy(previewCtx, client.ResolveGovernancePolicyInput{
		RequestType:              artifactString(body, "request_type"),
		ImpactLevel:              impactLevel,
		RequestedGovernanceLevel: artifactString(body, "requested_governance_level"),
		ImpactDeclaration:        impactDeclaration,
	})
}

func artifactString(body map[string]any, field string) string {
	value, _ := body[field].(string)
	return strings.TrimSpace(value)
}

func localArtifactSelection(
	ctx context.Context,
	deps *Deps,
	store *local.Store,
	body map[string]any,
) (local.Selection, error) {
	selection, err := localSelection(ctx, deps, store)
	if err != nil {
		return local.Selection{}, err
	}
	workspaceID := artifactString(body, "workspace_id")
	if workspaceID == "" {
		return selection, nil
	}
	selection.Workspace, err = store.Workspace(ctx, workspaceID)
	return selection, err
}

func missingArtifactRoles(body map[string]any, required []string) []string {
	present := map[string]bool{}
	if documents, ok := body["documents"].([]any); ok {
		for _, item := range documents {
			if document, ok := item.(map[string]any); ok {
				present[artifactString(document, "role")] = true
			}
		}
	}
	missing := []string{}
	for _, role := range required {
		if !present[role] {
			missing = append(missing, role)
		}
	}
	return missing
}

func localArtifactInput(body map[string]any) (local.ArtifactInput, error) {
	input := local.ArtifactInput{}
	input.FeatureKey, _ = body["feature_key"].(string)
	input.RequestType, _ = body["request_type"].(string)
	rawDocuments, ok := body["documents"].([]any)
	if !ok {
		return input, fmt.Errorf("documents must be an array")
	}
	for _, raw := range rawDocuments {
		document, ok := raw.(map[string]any)
		if !ok {
			return input, fmt.Errorf("documents must contain objects")
		}
		path, _ := document["path"].(string)
		role, _ := document["role"].(string)
		content, _ := document["content"].(string)
		input.Documents = append(input.Documents, local.ArtifactDocumentInput{Path: path, Role: role, Content: []byte(content)})
	}
	return input, nil
}

func localArtifactComparisonBase(artifact local.Artifact) (*client.Artifact, []client.ArtifactFile) {
	base := &client.Artifact{
		ID:             artifact.ID,
		WorkspaceID:    artifact.WorkspaceID,
		Version:        "v" + strconv.Itoa(artifact.Version),
		Status:         artifact.Status,
		RequestType:    artifact.RequestType,
		SnapshotDigest: artifact.SnapshotDigest,
		CreatedAt:      artifact.CreatedAt,
	}
	files := make([]client.ArtifactFile, 0, len(artifact.Documents))
	for _, document := range artifact.Documents {
		files = append(files, client.ArtifactFile{
			Path:          document.Path,
			Role:          document.Role,
			SizeBytes:     int64(document.SizeBytes),
			ContentSHA256: document.Digest,
		})
	}
	return base, files
}

func localArtifactView(artifact local.Artifact, includeContent bool) map[string]any {
	view := map[string]any{"id": artifact.ID, "workspace_id": artifact.WorkspaceID, "feature_key": artifact.FeatureKey, "request_type": artifact.RequestType, "version": artifact.Version, "status": artifact.Status, "snapshot_digest": artifact.SnapshotDigest, "created_at": artifact.CreatedAt}
	if includeContent {
		documents := make([]map[string]any, 0, len(artifact.Documents))
		for _, document := range artifact.Documents {
			documents = append(documents, map[string]any{"path": document.Path, "role": document.Role, "content": string(document.Content), "digest": document.Digest, "size_bytes": document.SizeBytes})
		}
		view["documents"] = documents
	}
	return view
}

func artifactPublishPreview(body map[string]any, sources []string) map[string]any {
	documents := []map[string]any{}
	if raw, ok := body["documents"].([]any); ok {
		for index, item := range raw {
			doc, ok := item.(map[string]any)
			if !ok {
				continue
			}
			content, _ := doc["content"].(string)
			row := map[string]any{
				"path": doc["path"], "role": doc["role"], "size_bytes": len(content),
			}
			if index < len(sources) && sources[index] != "" {
				row["source_path"] = sources[index]
			}
			documents = append(documents, row)
		}
	}
	base, _ := body["base_version"].(string)
	target := body["feature_key"]
	if target == nil {
		target = body["feature_id"]
	}
	omitted := []string{}
	if _, declared := body["impact_declaration"]; !declared {
		omitted = append(omitted, "impact_declaration")
	}
	preview := map[string]any{
		"source_kind": body["source_kind"], "source_id": body["source_id"], "source_revision": body["source_revision"],
		"documents": documents, "target": target, "base_version": base, "new_artifact": base == "",
		"omitted": omitted, "ambiguous": []string{}, "human_confirmation_required": true,
		"non_goals": []string{"No filesystem watcher", "No implicit repository-wide upload"},
	}
	if len(omitted) > 0 {
		// Name the fields, not just the gap. An author cannot answer "supply an
		// impact declaration", and an agent is right to refuse to invent a
		// governance answer on the human's behalf.
		preview["governance_hint"] = "Impact declaration missing; Full mode may select stricter governance. " +
			"To declare it, add `impact_level` (low|medium|high) and an `impact_declaration` object answering " +
			"`protected_domains_status`, `data_or_schema_change`, `external_contract_change`, " +
			"`irreversible_or_complex_rollback`, and `broad_blast_radius` with yes, no, or unknown. " +
			"Omitting it is allowed; unanswered questions resolve as unknown, which raises governance rather than lowering it."
	}
	return preview
}

func normalizeArtifactPublishBody(body map[string]any) error {
	if _, ok := body["version"]; ok {
		return fmt.Errorf("version is server-assigned; remove version from the publish file and use base_version only when publishing an update")
	}
	if _, hasRequestType := body["request_type"]; !hasRequestType {
		body["request_type"] = "unknown"
	}
	if requestType, ok := body["request_type"].(string); ok {
		body["request_type"] = strings.TrimSpace(requestType)
	}
	return nil
}

func validateArtifactPublishFields(body map[string]any) error {
	allowed := map[string]bool{
		"feature_key": true, "feature_name": true, "workspace_id": true,
		"base_version": true, "documents": true, "source_kind": true,
		"source_revision": true, "source_id": true, "created_by": true,
		"impact_level": true, "request_type": true, "authority": true,
		"requested_governance_level": true, "impact_declaration": true,
	}
	var unknown []string
	for field := range body {
		if !allowed[field] {
			unknown = append(unknown, field)
		}
	}
	slices.Sort(unknown)
	if len(unknown) > 0 {
		return fmt.Errorf("unknown artifact package field %q", unknown[0])
	}
	requestType, ok := body["request_type"].(string)
	if !ok || !slices.Contains([]string{"new_feature", "change_request", "bugfix", "unknown"}, strings.TrimSpace(requestType)) {
		return fmt.Errorf("request_type must be new_feature, change_request, bugfix, or unknown")
	}
	documents, ok := body["documents"].([]any)
	if !ok {
		return nil
	}
	allowedDocument := map[string]bool{
		"path": true, "role": true, "content": true, "source_file": true, "repo_file": true, "file_url": true,
	}
	for index, raw := range documents {
		document, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		unknown = unknown[:0]
		for field := range document {
			if !allowedDocument[field] {
				unknown = append(unknown, field)
			}
		}
		slices.Sort(unknown)
		if len(unknown) > 0 {
			return fmt.Errorf("unknown artifact package field %q", fmt.Sprintf("documents[%d].%s", index, unknown[0]))
		}
		sourceFields := make([]string, 0, 4)
		for _, field := range []string{"content", "source_file", "repo_file", "file_url"} {
			if _, exists := document[field]; exists {
				sourceFields = append(sourceFields, field)
			}
		}
		if len(sourceFields) == 0 {
			return fmt.Errorf("documents[%d] must set exactly one of content, source_file, repo_file, or file_url", index)
		}
		if len(sourceFields) > 1 {
			return fmt.Errorf("documents[%d] must set exactly one of content, source_file, repo_file, or file_url; found %s", index, strings.Join(sourceFields, ", "))
		}
		if _, ok := document[sourceFields[0]].(string); !ok {
			return fmt.Errorf("documents[%d].%s must be a string", index, sourceFields[0])
		}
	}
	return nil
}

// specgate artifact approve <artifact-id> [--note <text>]
