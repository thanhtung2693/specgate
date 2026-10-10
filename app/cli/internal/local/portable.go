package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/specgate/specgate/app/cli/internal/provenance"
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
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return PortableWorkspace{}, err
	}
	defer tx.Rollback()
	var pinned int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM verification_contracts WHERE workspace_id = ?`, workspaceID).Scan(&pinned); err != nil {
		return PortableWorkspace{}, err
	}
	if pinned > 0 {
		return PortableWorkspace{}, fmt.Errorf("%w: portable/v1 Full-mode import cannot preserve Local verification contracts; use a Local database backup instead", ErrVerificationInvalid)
	}
	var checkpoints int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM work_checkpoints WHERE workspace_id = ?`, workspaceID).Scan(&checkpoints); err != nil {
		return PortableWorkspace{}, err
	}
	if checkpoints > 0 {
		return PortableWorkspace{}, fmt.Errorf("%w: portable/v1 Full-mode import cannot preserve Local checkpoints; use a Local database backup instead", ErrVerificationInvalid)
	}
	var enhanced int
	if err := tx.QueryRowContext(ctx, `SELECT
	 (SELECT COUNT(*) FROM acceptance_bases WHERE workspace_id=?) +
	 (SELECT COUNT(*) FROM artifacts WHERE workspace_id=? AND source_lineage_json!='')`, workspaceID, workspaceID).Scan(&enhanced); err != nil {
		return PortableWorkspace{}, err
	}
	if enhanced > 0 {
		return PortableWorkspace{}, fmt.Errorf("%w: portable/v1 Full-mode import cannot preserve Local lineage or acceptance bases; use a Local database backup instead", ErrVerificationInvalid)
	}
	workspace, err := workspaceByRef(ctx, tx, workspaceID)
	if err != nil {
		return PortableWorkspace{}, err
	}
	out := PortableWorkspace{Workspace: workspace}
	features, err := listFeatures(ctx, tx, workspaceID)
	if err != nil {
		return out, err
	}
	for _, feature := range features {
		out.Features = append(out.Features, PortableFeature{
			ID: feature.ID, Key: feature.Key, CanonicalArtifactID: feature.CanonicalArtifactID, Version: feature.Version,
		})
	}
	artifacts, err := listArtifacts(ctx, tx, workspaceID)
	if err != nil {
		return out, err
	}
	for _, artifact := range artifacts {
		if len(artifact.SourceCriteria) > 0 {
			return PortableWorkspace{}, fmt.Errorf("%w: portable/v1 Full-mode import cannot preserve Local source criteria; use a Local database backup instead", ErrVerificationInvalid)
		}
	}
	for index := len(artifacts) - 1; index >= 0; index-- {
		artifact, err := getArtifact(ctx, tx, workspaceID, artifacts[index].ID)
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
	out.Work, err = listWork(ctx, tx, workspaceID)
	if err != nil {
		return out, err
	}
	if out.Gates, err = exportGateEvidence(ctx, tx, workspaceID); err != nil {
		return out, err
	}
	if out.Delivery, err = exportDeliveryEvidence(ctx, tx, workspaceID); err != nil {
		return out, err
	}
	return out, nil
}

func exportGateEvidence(ctx context.Context, q artifactQueryer, workspaceID string) ([]PortableGateEvidence, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, artifact_id, gate_key, gate_version, gate_digest, artifact_digest, policy_digest, executor,
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

func exportDeliveryEvidence(ctx context.Context, q artifactQueryer, workspaceID string) ([]PortableDeliveryEvidence, error) {
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
	reportRows, err := q.QueryContext(ctx, `SELECT id, work_id, body FROM delivery_reports WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID)
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
		row.Report = nil
		if err := json.Unmarshal([]byte(body), &row.Report); err != nil {
			reportRows.Close()
			return nil, err
		}
		row.Report = provenance.Receipts(row.Report)
	}
	if err := reportRows.Close(); err != nil {
		return nil, err
	}
	if err := reportRows.Err(); err != nil {
		return nil, err
	}
	reviewRows, err := q.QueryContext(ctx, `SELECT r.id, r.work_id, r.report_id, r.verdict, r.summary, r.human_decision, r.note, p.body
		FROM delivery_reviews r LEFT JOIN delivery_reports p
		ON p.id = r.report_id AND p.workspace_id = r.workspace_id AND p.work_id = r.work_id
		WHERE r.workspace_id = ? ORDER BY r.created_at, r.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	for reviewRows.Next() {
		var id, workID, reportID, verdict, summary, decision, note string
		var body sql.NullString
		if err := reviewRows.Scan(&id, &workID, &reportID, &verdict, &summary, &decision, &note, &body); err != nil {
			reviewRows.Close()
			return nil, err
		}
		row := get(workID)
		row.ReportID = reportID
		row.Report = nil
		if reportID != "" && !body.Valid {
			reviewRows.Close()
			return nil, fmt.Errorf("delivery review %s has no matching report %s", id, reportID)
		}
		if body.Valid {
			if err := json.Unmarshal([]byte(body.String), &row.Report); err != nil {
				reviewRows.Close()
				return nil, err
			}
			row.Report = provenance.Receipts(row.Report)
		}
		row.ReviewID, row.Verdict, row.Summary, row.HumanDecision, row.ReviewNote = id, verdict, summary, decision, note
	}
	if err := reviewRows.Close(); err != nil {
		return nil, err
	}
	if err := reviewRows.Err(); err != nil {
		return nil, err
	}
	peerRows, err := q.QueryContext(ctx, `SELECT id, work_id, agent_name, body FROM delivery_peer_reviews WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID)
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
		row.PeerReview = nil
		if err := json.Unmarshal([]byte(body), &row.PeerReview); err != nil {
			peerRows.Close()
			return nil, err
		}
		row.PeerReview = provenance.Receipts(row.PeerReview)
	}
	if err := peerRows.Close(); err != nil {
		return nil, err
	}
	if err := peerRows.Err(); err != nil {
		return nil, err
	}
	result := make([]PortableDeliveryEvidence, 0, len(order))
	for _, workID := range order {
		result = append(result, *byWork[workID])
	}
	return result, nil
}
