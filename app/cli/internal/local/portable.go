package local

import (
	"context"
	"database/sql"
	"encoding/json"
)

type PortableWorkspace struct {
	Workspace Workspace                  `json:"workspace"`
	Features  []PortableFeature          `json:"features"`
	Artifacts []PortableArtifact         `json:"artifacts"`
	Work      []WorkItem                 `json:"work"`
	Gates     []PortableGateEvidence     `json:"gates"`
	Delivery  []PortableDeliveryEvidence `json:"delivery"`
}

type PortableFeature struct {
	ID                  string `json:"id"`
	Key                 string `json:"key"`
	CanonicalArtifactID string `json:"canonical_artifact_id"`
	Version             int    `json:"version"`
}

type PortableArtifact struct {
	ID             string                     `json:"id"`
	FeatureKey     string                     `json:"feature_key"`
	RequestType    string                     `json:"request_type"`
	Version        int                        `json:"version"`
	Status         string                     `json:"status"`
	SnapshotDigest string                     `json:"snapshot_digest"`
	PolicyDigest   string                     `json:"policy_digest"`
	PolicySnapshot string                     `json:"policy_snapshot"`
	CreatedAt      string                     `json:"created_at"`
	Documents      []PortableArtifactDocument `json:"documents"`
}

type PortableArtifactDocument struct {
	Path    string `json:"path"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Digest  string `json:"digest"`
}

type PortableGateEvidence struct {
	TaskID         string          `json:"task_id"`
	ArtifactID     string          `json:"artifact_id"`
	GateKey        string          `json:"gate_key"`
	GateVersion    string          `json:"gate_version"`
	GateDigest     string          `json:"gate_digest"`
	ArtifactDigest string          `json:"artifact_digest"`
	PolicyDigest   string          `json:"policy_digest"`
	Executor       string          `json:"executor"`
	ResultID       string          `json:"result_id,omitempty"`
	ResultState    string          `json:"result_state,omitempty"`
	ResultSummary  string          `json:"result_summary,omitempty"`
	Evaluator      json.RawMessage `json:"evaluator,omitempty"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	Findings       json.RawMessage `json:"findings,omitempty"`
	SubmittedAt    string          `json:"submitted_at,omitempty"`
}

type PortableDeliveryEvidence struct {
	WorkID        string         `json:"work_id"`
	ReportID      string         `json:"report_id,omitempty"`
	Report        map[string]any `json:"report,omitempty"`
	ReviewID      string         `json:"review_id,omitempty"`
	Verdict       string         `json:"verdict,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	HumanDecision string         `json:"human_decision,omitempty"`
	ReviewNote    string         `json:"review_note,omitempty"`
	PeerReviewID  string         `json:"peer_review_id,omitempty"`
	PeerAgent     string         `json:"peer_agent,omitempty"`
	PeerReview    map[string]any `json:"peer_review,omitempty"`
}

func (s *Store) ExportWorkspace(ctx context.Context, workspaceID string) (PortableWorkspace, error) {
	workspace, err := s.Workspace(ctx, workspaceID)
	if err != nil {
		return PortableWorkspace{}, err
	}
	out := PortableWorkspace{Workspace: workspace}
	features, err := s.ListFeatures(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	for _, feature := range features {
		out.Features = append(out.Features, PortableFeature{
			ID: feature.ID, Key: feature.Key, CanonicalArtifactID: feature.CanonicalArtifactID, Version: feature.Version,
		})
	}
	artifacts, err := s.ListArtifacts(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	for index := len(artifacts) - 1; index >= 0; index-- {
		artifact, err := s.GetArtifact(ctx, workspaceID, artifacts[index].ID)
		if err != nil {
			return out, err
		}
		item := PortableArtifact{
			ID: artifact.ID, FeatureKey: artifact.FeatureKey, RequestType: artifact.RequestType,
			Version: artifact.Version, Status: artifact.Status, SnapshotDigest: artifact.SnapshotDigest,
			PolicyDigest: artifact.PolicyDigest, PolicySnapshot: artifact.PolicySnapshot, CreatedAt: artifact.CreatedAt,
		}
		for _, document := range artifact.Documents {
			item.Documents = append(item.Documents, PortableArtifactDocument{
				Path: document.Path, Role: document.Role, Content: string(document.Content), Digest: document.Digest,
			})
		}
		out.Artifacts = append(out.Artifacts, item)
	}
	out.Work, err = s.ListWork(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	if out.Gates, err = s.exportGateEvidence(ctx, workspaceID); err != nil {
		return out, err
	}
	if out.Delivery, err = s.exportDeliveryEvidence(ctx, workspaceID); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) exportGateEvidence(ctx context.Context, workspaceID string) ([]PortableGateEvidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, artifact_id, gate_key, gate_version, gate_digest, artifact_digest, policy_digest, executor,
		result_id, result_state, result_summary, evaluator_json, evidence_json, findings_json, submitted_at
		FROM local_gate_tasks WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PortableGateEvidence
	for rows.Next() {
		var row PortableGateEvidence
		var resultID, state, summary, evaluator, evidence, findings, submitted sql.NullString
		if err := rows.Scan(
			&row.TaskID, &row.ArtifactID, &row.GateKey, &row.GateVersion, &row.GateDigest,
			&row.ArtifactDigest, &row.PolicyDigest, &row.Executor,
			&resultID, &state, &summary, &evaluator, &evidence, &findings, &submitted,
		); err != nil {
			return nil, err
		}
		row.ResultID, row.ResultState, row.ResultSummary, row.SubmittedAt = resultID.String, state.String, summary.String, submitted.String
		if evaluator.Valid {
			row.Evaluator = json.RawMessage(evaluator.String)
		}
		if evidence.Valid {
			row.Evidence = json.RawMessage(evidence.String)
		}
		if findings.Valid {
			row.Findings = json.RawMessage(findings.String)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) exportDeliveryEvidence(ctx context.Context, workspaceID string) ([]PortableDeliveryEvidence, error) {
	byWork := map[string]*PortableDeliveryEvidence{}
	order := []string{}
	get := func(workID string) *PortableDeliveryEvidence {
		if row := byWork[workID]; row != nil {
			return row
		}
		row := &PortableDeliveryEvidence{WorkID: workID}
		byWork[workID] = row
		order = append(order, workID)
		return row
	}
	reportRows, err := s.db.QueryContext(ctx, `SELECT id, work_id, body FROM delivery_reports WHERE workspace_id = ? ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	for reportRows.Next() {
		var id, workID, body string
		if err := reportRows.Scan(&id, &workID, &body); err != nil {
			reportRows.Close()
			return nil, err
		}
		row := get(workID)
		row.ReportID = id
		if err := json.Unmarshal([]byte(body), &row.Report); err != nil {
			reportRows.Close()
			return nil, err
		}
	}
	if err := reportRows.Close(); err != nil {
		return nil, err
	}
	reviewRows, err := s.db.QueryContext(ctx, `SELECT id, work_id, report_id, verdict, summary, human_decision, note FROM delivery_reviews WHERE workspace_id = ? ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	for reviewRows.Next() {
		var id, workID, reportID, verdict, summary, decision, note string
		if err := reviewRows.Scan(&id, &workID, &reportID, &verdict, &summary, &decision, &note); err != nil {
			reviewRows.Close()
			return nil, err
		}
		row := get(workID)
		row.ReportID = reportID
		row.ReviewID, row.Verdict, row.Summary, row.HumanDecision, row.ReviewNote = id, verdict, summary, decision, note
	}
	if err := reviewRows.Close(); err != nil {
		return nil, err
	}
	peerRows, err := s.db.QueryContext(ctx, `SELECT id, work_id, agent_name, body FROM delivery_peer_reviews WHERE workspace_id = ? ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	for peerRows.Next() {
		var id, workID, agent, body string
		if err := peerRows.Scan(&id, &workID, &agent, &body); err != nil {
			peerRows.Close()
			return nil, err
		}
		row := get(workID)
		row.PeerReviewID, row.PeerAgent = id, agent
		if err := json.Unmarshal([]byte(body), &row.PeerReview); err != nil {
			peerRows.Close()
			return nil, err
		}
	}
	if err := peerRows.Close(); err != nil {
		return nil, err
	}
	result := make([]PortableDeliveryEvidence, 0, len(order))
	for _, workID := range order {
		result = append(result, *byWork[workID])
	}
	return result, nil
}
