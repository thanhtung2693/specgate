// Package workflow contains Local CLI JSON projections retained for existing consumers.
package workflow

type PluginPackage struct {
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	ServedFiles []string `json:"served_files"`
}

type GateTask struct {
	TaskID         string `json:"task_id"`
	WorkspaceID    string `json:"workspace_id"`
	GateKey        string `json:"gate_key"`
	GateVersion    string `json:"gate_version"`
	GateDigest     string `json:"gate_digest"`
	ArtifactID     string `json:"artifact_id"`
	ArtifactDigest string `json:"artifact_digest"`
	PolicyDigest   string `json:"policy_digest"`
	Executor       string `json:"executor"`
	SkillContent   string `json:"skill_content,omitempty"`
	ExpiresAt      string `json:"expires_at"`
}

type DispatchGateTasksResult struct {
	ArtifactID      string   `json:"artifact_id"`
	CreatedTaskIDs  []string `json:"created_task_ids"`
	SkippedGateKeys []string `json:"skipped_gate_keys"`
	PendingTaskIDs  []string `json:"pending_task_ids"`
}

type CriterionReview struct {
	CriterionID         string `json:"criterion_id,omitempty"`
	Text                string `json:"text,omitempty"`
	Verdict             string `json:"verdict,omitempty"`
	Why                 string `json:"why,omitempty"`
	VerificationBinding string `json:"verification_binding,omitempty"`
}

type CheckResult struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
	Source string `json:"source,omitempty"`
}

type Feature struct {
	ID                  string `json:"id"`
	WorkspaceID         string `json:"workspace_id,omitempty"`
	Key                 string `json:"key"`
	Name                string `json:"name"`
	Status              string `json:"status"`
	Version             int    `json:"version"`
	CanonicalArtifactID string `json:"canonical_artifact_id,omitempty"`
	UpdatedAt           string `json:"updated_at"`
}

type PolicyExplanation struct {
	GovernanceLevel string   `json:"governance_level"`
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	Reasons         []string `json:"reasons,omitempty"`
}

type PolicyProjection struct {
	GovernanceLevel  string                   `json:"governance_level"`
	ReasonCodes      []string                 `json:"reason_codes"`
	RequiredRoles    []string                 `json:"required_roles"`
	RequiredTopics   []string                 `json:"required_topics"`
	RequiredEvidence []string                 `json:"required_evidence"`
	EnabledGates     []string                 `json:"enabled_gates"`
	ApprovalPolicy   string                   `json:"approval_policy"`
	EvidencePolicy   string                   `json:"evidence_policy"`
	PolicyDigest     string                   `json:"policy_digest"`
	Rubrics          []PolicyRubricProjection `json:"rubrics"`
	Explanation      PolicyExplanation        `json:"explanation"`
}

type PolicyRubricProjection struct {
	Gate   string `json:"gate"`
	Skill  string `json:"skill,omitempty"`
	Digest string `json:"digest,omitempty"`
	Source string `json:"source"`
}
