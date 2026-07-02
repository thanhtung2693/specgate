package governanceprofile

import (
	"encoding/json"
	"errors"
)

// ErrUnsupportedSnapshot is returned by ParseSnapshot when the snapshot carries an
// explicit snapshot_schema_version that is not recognised. Callers must treat this
// as fail-closed (i.e. require human approval).
var ErrUnsupportedSnapshot = errors.New("unsupported snapshot schema version")

// Schema-version discriminator constants.
const (
	// SnapshotSchemaLegacyV1 is assigned to snapshots that carry no
	// snapshot_schema_version field (legacy ResolvedProfile shape).
	SnapshotSchemaLegacyV1 = "legacy/v1"
	// SnapshotSchemaPolicyV1 is the first versioned envelope format.
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

// PolicyLineageEntry records a profile version in the resolution chain.
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
	RendererKey              string               `json:"renderer_key,omitempty"`
	ApprovalPolicy           string               `json:"approval_policy"`
	EvidencePolicy           string               `json:"evidence_policy"`
	GateSkills               map[string]string    `json:"gate_skills,omitempty"`
	PolicyLineage            []PolicyLineageEntry `json:"policy_lineage,omitempty"`
	Digest                   string               `json:"digest,omitempty"`
}

// ParsedSnapshot is the unified execution-projection produced by ParseSnapshot. It
// holds only the fields that downstream logic (approval gate, readiness engine) needs,
// regardless of which snapshot schema version was stored.
type ParsedSnapshot struct {
	// SchemaVersion is SnapshotSchemaLegacyV1 or SnapshotSchemaPolicyV1.
	SchemaVersion string
	// ApprovalPolicy is the raw stored value; may be empty. Callers use
	// EffectiveApprovalPolicy(snap.ApprovalPolicy) to apply the human_required default.
	ApprovalPolicy string
	// GovernanceLevel is only populated for v1 snapshots.
	GovernanceLevel GovernanceLevel
	// EnabledGates is populated from either legacy or v1 shape.
	EnabledGates []string
	// RequiredRoles is populated from either shape.
	RequiredRoles []string
	// RequiredTopics is populated from either shape.
	RequiredTopics []string
	// EvidencePolicy is the raw stored value; may be empty. Callers use
	// EffectiveEvidencePolicy(snap.EvidencePolicy) to apply the attested_ok default.
	EvidencePolicy string
	// GateSkills is populated from either shape.
	GateSkills map[string]string
}

// snapshotProbe is used to peek at the snapshot_schema_version field before
// deciding which typed struct to unmarshal into. This avoids confusing the
// legacy "version" field (a semver like "v1.0.0") with the schema discriminator.
type snapshotProbe struct {
	SnapshotSchemaVersion string `json:"snapshot_schema_version"`
}

// ParseSnapshot parses a governance gates profile snapshot JSON string and returns a
// unified ParsedSnapshot. The function is fail-closed:
//
//   - Empty input → zero ParsedSnapshot, no error.
//   - No snapshot_schema_version field → treated as legacy (SnapshotSchemaLegacyV1).
//   - snapshot_schema_version == "specgate.policy/v1" → v1 path.
//   - Any other explicit version → ErrUnsupportedSnapshot.
//   - Corrupt JSON on either path → the relevant unmarshal error (fail-closed).
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
	case "", SnapshotSchemaLegacyV1:
		// Missing or explicit legacy marker → legacy ResolvedProfile shape.
		return parseLegacySnapshot(raw)
	case SnapshotSchemaPolicyV1:
		return parsePolicyV1Snapshot(raw)
	default:
		return ParsedSnapshot{}, ErrUnsupportedSnapshot
	}
}

// parseLegacySnapshot unmarshals a legacy ResolvedProfile JSON and projects it into
// ParsedSnapshot.
func parseLegacySnapshot(raw string) (ParsedSnapshot, error) {
	var rp ResolvedProfile
	if err := json.Unmarshal([]byte(raw), &rp); err != nil {
		return ParsedSnapshot{}, err
	}
	return ParsedSnapshot{
		SchemaVersion:  SnapshotSchemaLegacyV1,
		ApprovalPolicy: rp.ApprovalPolicy,
		EnabledGates:   rp.EnabledGates,
		RequiredRoles:  rp.RequiredRoles,
		RequiredTopics: rp.RequiredTopics,
		EvidencePolicy: rp.EvidencePolicy,
		GateSkills:     rp.GateSkills,
	}, nil
}

// parsePolicyV1Snapshot unmarshals a specgate.policy/v1 envelope and projects it into
// ParsedSnapshot.
func parsePolicyV1Snapshot(raw string) (ParsedSnapshot, error) {
	var rs ResolvedSnapshot
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		return ParsedSnapshot{}, err
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
	}, nil
}
