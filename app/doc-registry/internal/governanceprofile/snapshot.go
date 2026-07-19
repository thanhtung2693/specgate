package governanceprofile

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnsupportedSnapshot is returned by ParseSnapshot when the snapshot carries an
// explicit snapshot_schema_version that is not recognised. Callers must treat this
// as fail-closed (i.e. require human approval).
var ErrUnsupportedSnapshot = errors.New("unsupported snapshot schema version")

// ErrInvalidSnapshot is returned when a recognized snapshot omits or carries
// an unsupported required policy value.
var ErrInvalidSnapshot = errors.New("invalid policy snapshot")

// Schema-version discriminator constants.
const (
	// SnapshotSchemaPolicyV1 is the required versioned envelope format.
	SnapshotSchemaPolicyV1 = "specgate.policy/v1"
)

// GovernanceLevel is the governance intensity tier, defined here so ParseSnapshot
// can project it.
type GovernanceLevel string

const (
	GovernanceLight    GovernanceLevel = "light"
	GovernanceStandard GovernanceLevel = "standard"
	GovernanceEnhanced GovernanceLevel = "enhanced"
)

// PolicyLineageEntry records one automatic policy step in the resolution chain.
type PolicyLineageEntry struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	Digest  string `json:"digest,omitempty"`
}

// ResolvedSnapshot is the typed envelope for specgate.policy/v1 snapshots.
// Fields match the spec §extensible-governance-snapshot contract.
type ResolvedSnapshot struct {
	SchemaVersion            string               `json:"snapshot_schema_version"`
	WorkType                 string               `json:"work_type,omitempty"`
	RiskLevel                string               `json:"risk_level,omitempty"`
	RequestedGovernanceLevel GovernanceLevel      `json:"requested_governance_level,omitempty"`
	GovernanceLevel          GovernanceLevel      `json:"governance_level,omitempty"`
	ReasonCodes              []string             `json:"reason_codes,omitempty"`
	RequiredRoles            []string             `json:"required_roles"`
	RequiredTopics           []string             `json:"required_topics"`
	RequiredEvidence         []string             `json:"required_evidence"`
	EnabledGates             []string             `json:"enabled_gates"`
	ApprovalPolicy           string               `json:"approval_policy"`
	EvidencePolicy           string               `json:"evidence_policy"`
	GateSkills               map[string]string    `json:"gate_skills,omitempty"`
	GateDefinitions          []GateDefinition     `json:"gate_definitions,omitempty"`
	PolicyLineage            []PolicyLineageEntry `json:"policy_lineage,omitempty"`
	Digest                   string               `json:"digest,omitempty"`
}

// ParsedSnapshot is the unified execution-projection produced by ParseSnapshot. It
// holds only the fields that downstream logic (approval gate, readiness engine) needs.
type ParsedSnapshot struct {
	// SchemaVersion is SnapshotSchemaPolicyV1 for a non-empty snapshot.
	SchemaVersion string
	// ApprovalPolicy is the frozen approval contract.
	ApprovalPolicy string
	// GovernanceLevel is only populated for v1 snapshots.
	GovernanceLevel GovernanceLevel
	// EnabledGates is projected from the versioned snapshot.
	EnabledGates []string
	// RequiredRoles is projected from the versioned snapshot.
	RequiredRoles []string
	// RequiredTopics is populated from either shape.
	RequiredTopics []string
	// EvidencePolicy is the frozen delivery-evidence contract.
	EvidencePolicy string
	// GateSkills is projected from the versioned snapshot.
	GateSkills map[string]string
	// GateDefinitions is the frozen runtime rubric contract.
	GateDefinitions []GateDefinition
}

// snapshotProbe peeks at the required schema discriminator before unmarshalling
// the versioned envelope.
type snapshotProbe struct {
	SnapshotSchemaVersion string `json:"snapshot_schema_version"`
}

// ParseSnapshot parses a governance policy snapshot JSON string and returns a
// unified ParsedSnapshot. The function is fail-closed:
//
//   - Empty input → zero ParsedSnapshot, no error.
//   - snapshot_schema_version == "specgate.policy/v1" → v1 path.
//   - Any other or missing version → ErrUnsupportedSnapshot.
//   - Corrupt JSON → the unmarshal error (fail-closed).
func ParseSnapshot(raw string) (ParsedSnapshot, error) {
	if raw == "" {
		return ParsedSnapshot{}, nil
	}

	// Peek at the discriminator field only.
	var probe snapshotProbe
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return ParsedSnapshot{}, err
	}

	switch probe.SnapshotSchemaVersion {
	case SnapshotSchemaPolicyV1:
		return parsePolicyV1Snapshot(raw)
	default:
		return ParsedSnapshot{}, ErrUnsupportedSnapshot
	}
}

// parsePolicyV1Snapshot unmarshals a specgate.policy/v1 envelope and projects it into
// ParsedSnapshot.
func parsePolicyV1Snapshot(raw string) (ParsedSnapshot, error) {
	var rs ResolvedSnapshot
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		return ParsedSnapshot{}, err
	}
	if !KnownApprovalPolicies[rs.ApprovalPolicy] {
		return ParsedSnapshot{}, fmt.Errorf("%w: unsupported approval_policy %q", ErrInvalidSnapshot, rs.ApprovalPolicy)
	}
	if !KnownEvidencePolicies[rs.EvidencePolicy] {
		return ParsedSnapshot{}, fmt.Errorf("%w: unsupported evidence_policy %q", ErrInvalidSnapshot, rs.EvidencePolicy)
	}
	return ParsedSnapshot{
		SchemaVersion:   SnapshotSchemaPolicyV1,
		ApprovalPolicy:  rs.ApprovalPolicy,
		GovernanceLevel: rs.GovernanceLevel,
		EnabledGates:    rs.EnabledGates,
		RequiredRoles:   rs.RequiredRoles,
		RequiredTopics:  rs.RequiredTopics,
		EvidencePolicy:  rs.EvidencePolicy,
		GateSkills:      rs.GateSkills,
		GateDefinitions: rs.GateDefinitions,
	}, nil
}
