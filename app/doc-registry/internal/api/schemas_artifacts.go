package api

import "time"

// ArtifactDTO is the OpenAPI-facing representation of an artifact.
// Domain model (internal/artifact.Artifact) stays free of API concerns.
type ArtifactDTO struct {
	ID                   string   `json:"id" example:"8f2c1b..." doc:"Artifact UUID"`
	WorkspaceID          string   `json:"workspace_id,omitempty" doc:"Owning workspace for governed artifacts"`
	FeatureID            string   `json:"feature_id" example:"checkout-loyalty-points"`
	Version              string   `json:"version" example:"v0.3" pattern:"^v\\d+\\.\\d+$"`
	Status               string   `json:"status" enum:"draft,needs_changes,approved,superseded"`
	RequestType          string   `json:"request_type" enum:"new_feature,change_request,bugfix,unknown"`
	ImpactLevel          string   `json:"impact_level" enum:"low,medium,high"`
	ArtifactPhase        string   `json:"artifact_phase,omitempty" enum:"phase1,phase2"`
	ArtifactCompleteness string   `json:"artifact_completeness,omitempty" enum:"partial,full"`
	ConfidenceScore      *float64 `json:"confidence_score,omitempty" minimum:"0" maximum:"1"`
	AmbiguityScore       *float64 `json:"ambiguity_score,omitempty" minimum:"0" maximum:"1"`
	GovernanceVersion    string   `json:"governance_version,omitempty"`
	PolicyVersion        string   `json:"policy_version,omitempty"`
	PolicyDigest         string   `json:"policy_digest,omitempty"`
	PolicySnapshot       string   `json:"policy_snapshot_json,omitempty"`
	// ExpectedGates is the enabled-gate set derived from the policy snapshot
	// — read-only, so the UI can show which gates a readiness run will execute
	// before any run exists. Empty when no snapshot is persisted.
	ExpectedGates []string `json:"expected_gates,omitempty"`
	// Publication lineage — origin system and VCS context at publish time.
	SourceKind     string `json:"source_kind,omitempty" doc:"Publication origin kind (e.g. specgate-ide, governance)"`
	SourceID       string `json:"source_id,omitempty" doc:"Origin system artifact or issue identifier"`
	SourceRevision string `json:"source_revision,omitempty" doc:"VCS revision or version at publish time"`
	SnapshotDigest string `json:"snapshot_digest,omitempty" doc:"Server-computed digest of the path/role/content manifest"`
	// FeatureName is the human-readable display name of the associated feature
	// (populated via JOIN with the features table; empty when no feature record exists).
	FeatureName      string       `json:"feature_name,omitempty" doc:"Display name of the feature this artifact belongs to"`
	CreatedBy        string       `json:"created_by"`
	ApprovedBy       string       `json:"approved_by,omitempty"`
	ApprovedAt       *time.Time   `json:"approved_at,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	ImpactedServices []ServiceDTO `json:"impacted_services,omitempty"`
}

type ServiceDTO struct {
	Name string `json:"name"`
	Kind string `json:"kind" enum:"service,app"`
}

// ---------- GET /artifacts ----------

type ListArtifactsInput struct {
	WorkspaceID string `query:"workspace_id"`
	FeatureID   string `query:"feature_id"`
	Service     string `query:"service"`
	Status      string `query:"status" enum:"draft,needs_changes,approved,superseded"`
	// ExcludeStatus drops one status server-side (default "current" list
	// views exclude superseded). Ignored when status is also set.
	ExcludeStatus string `query:"exclude_status" enum:"draft,needs_changes,approved,superseded"`
	Limit         int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
	Offset        int    `query:"offset" minimum:"0" default:"0"`
}

type ListArtifactsOutput struct {
	Body struct {
		Items []ArtifactDTO `json:"items"`
		Total int           `json:"total" doc:"Absolute count of artifacts matching the filter, ignoring limit/offset."`
	}
}

// ---------- GET /artifacts/{id} ----------

type GetArtifactInput struct {
	ID          string `path:"id" doc:"Artifact UUID"`
	WorkspaceID string `query:"workspace_id"`
}

type GetArtifactOutput struct {
	Body ArtifactDTO
}

// ---------- PATCH /artifacts/{id}/status ----------

type UpdateStatusInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Body        struct {
		Status       string `json:"status" required:"true" enum:"approved,needs_changes"`
		ApprovedBy   string `json:"approved_by,omitempty"`
		ReviewRating int    `json:"review_rating,omitempty" minimum:"1" maximum:"4" doc:"1=unusable, 4=ready"`
		Note         string `json:"note,omitempty"`
		// ActorKind identifies who is requesting the transition. Client-asserted
		// cooperative check — not server-side identity enforcement. Defaults to
		// "human" when absent so existing callers are unaffected.
		ActorKind string `json:"actor_kind,omitempty" enum:"human,agent"`
	}
}

type UpdateStatusOutput struct {
	Body ArtifactDTO
}

// ---------- GET /artifacts/{id}/files ----------

type ArtifactFileDTO struct {
	Path          string `json:"path"`
	Role          string `json:"role"`
	SizeBytes     int64  `json:"size_bytes"`
	ContentSHA256 string `json:"content_sha256"`
}

type ListArtifactFilesInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Role        string `query:"role" doc:"Optional: filter by role"`
}

type ListArtifactFilesOutput struct {
	Body struct {
		Items []ArtifactFileDTO `json:"items"`
	}
}

// ---------- POST/GET /artifacts/{id}/readiness-runs ----------

type ArtifactReadinessRunDTO struct {
	ID           string    `json:"id"`
	ArtifactID   string    `json:"artifact_id"`
	Gate         string    `json:"gate"`
	State        string    `json:"state" enum:"pass,warn,fail,needs_human_review,not_applicable,not_run"`
	Hint         string    `json:"hint"`
	Executor     string    `json:"executor,omitempty"`
	EvidenceJSON string    `json:"evidence_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type ListArtifactReadinessRunsInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Limit       int    `query:"limit"`
}

type ListArtifactReadinessRunsOutput struct {
	Body struct {
		Items []ArtifactReadinessRunDTO `json:"items"`
	}
}

type RefreshArtifactReadinessRunsInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Body        *struct {
		Evaluations []struct {
			Gate             string  `json:"gate"`
			State            string  `json:"state"`
			Hint             string  `json:"hint,omitempty"`
			Confidence       float64 `json:"confidence,omitempty"`
			JudgeModel       string  `json:"judge_model,omitempty"`
			EvalSuiteVersion string  `json:"eval_suite_version,omitempty"`
			Evidence         string  `json:"evidence,omitempty"`
		} `json:"evaluations,omitempty"`
	} `json:"body,omitempty"`
}

// ---------- GET /artifacts/{id}/files/_?path=... ----------

// GetArtifactFileInput accepts a document path via query param so slash-containing
// paths (e.g. docs/proposal.md) are not mangled by URL routing.
type GetArtifactFileInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Path        string `query:"path" required:"true" doc:"Document path within the artifact"`
}

type GetArtifactFileOutput struct {
	Body struct {
		SizeBytes int64  `json:"size_bytes"`
		Content   string `json:"content" doc:"Immutable UTF-8 artifact document content; each document is limited to 1 MiB"`
	}
}

// ---------- GET /conflicts ----------

type CheckConflictsInput struct {
	WorkspaceID        string   `query:"workspace_id"`
	Services           []string `query:"services" required:"true" doc:"Comma- or repeated-parameter list of impacted services"`
	CandidateFeatureID string   `query:"candidate_feature_id" doc:"Feature ID of the candidate being checked (populates feature_a in response)"`
	CandidateVersion   string   `query:"candidate_version" doc:"Version of the candidate being checked"`
	CandidateStatus    string   `query:"candidate_status" doc:"Status of the candidate being checked (default: draft)"`
}

type ConflictFeatureRefDTO struct {
	FeatureID string `json:"feature_id"`
	Version   string `json:"version"`
	Status    string `json:"status"`
}

type ConflictDTO struct {
	ConflictID          string                `json:"conflict_id"`
	Type                string                `json:"type" enum:"no_conflict,warning_conflict,blocking_conflict"`
	FeatureA            ConflictFeatureRefDTO `json:"feature_a"`
	FeatureB            ConflictFeatureRefDTO `json:"feature_b"`
	OverlappingServices []string              `json:"overlapping_services"`
	OverlappingModules  []string              `json:"overlapping_modules,omitempty"`
	ResolutionOptions   []string              `json:"resolution_options"`
}

type CheckConflictsOutput struct {
	Body struct {
		ConflictState string        `json:"conflict_state" enum:"no_conflict,warning_conflict,blocking_conflict"`
		Conflicts     []ConflictDTO `json:"conflicts"`
	}
}

// ---------- GET /events ----------

type ListEventsInput struct {
	WorkspaceID string    `query:"workspace_id"`
	EventType   string    `query:"event_type"`
	ArtifactID  string    `query:"artifact_id" doc:"Restrict events to one artifact"`
	After       time.Time `query:"after" doc:"Return events with created_at > after"`
	Limit       int       `query:"limit" minimum:"1" maximum:"500" default:"100"`
}

type EventDTO struct {
	ID         string         `json:"id"`
	ArtifactID string         `json:"artifact_id"`
	EventType  string         `json:"event_type" enum:"artifact.published,artifact.approved,artifact.needs_changes,artifact.superseded,feature.canonical_changed,change_request.acceptance_criteria_changed"`
	Payload    map[string]any `json:"payload"`
	CreatedAt  time.Time      `json:"created_at"`
}

type ListEventsOutput struct {
	Body struct {
		Items []EventDTO `json:"items"`
	}
}
