package api

import "time"

// ArtifactDTO is the OpenAPI-facing representation of an artifact.
// Domain model (internal/artifact.Artifact) stays free of API concerns.
type ArtifactDTO struct {
	ID                   string   `json:"id" example:"8f2c1b..." doc:"Artifact UUID"`
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
	GatesProfile         string   `json:"gates_profile,omitempty"`
	GatesProfileVersion  string   `json:"gates_profile_version,omitempty"`
	GatesProfileDigest   string   `json:"gates_profile_digest,omitempty"`
	GatesProfileSnapshot string   `json:"gates_profile_snapshot_json,omitempty"`
	// ExpectedGates is the enabled-gate set derived from the gates-profile snapshot
	// — read-only, so the UI can show which gates a readiness run will execute
	// before any run exists. Empty when no snapshot is persisted.
	ExpectedGates []string `json:"expected_gates,omitempty"`
	// Publication lineage — origin system and VCS context at publish time.
	SourceKind     string `json:"source_kind,omitempty" doc:"Publication origin kind (e.g. specgate-ide, governance)"`
	SourceID       string `json:"source_id,omitempty" doc:"Origin system artifact or issue identifier"`
	SourceRevision string `json:"source_revision,omitempty" doc:"VCS revision or version at publish time"`
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

// DocumentInputDTO is the API-facing shape for a single document in an artifact package.
// Either content (base64) or ref (governance file id) must be set.
type DocumentInputDTO struct {
	Path    string `json:"path" required:"true" doc:"Document path within the artifact, e.g. prd.md or docs/proposal.md"`
	Role    string `json:"role,omitempty" doc:"Document role: spec, design, plan, verification, research, reference, unspecified, or custom:<name>"`
	Content string `json:"content,omitempty" doc:"Base64-encoded document content; mutually exclusive with ref"`
	Ref     string `json:"ref,omitempty" doc:"Committed governance file id; content takes precedence if both provided"`
}

// ---------- POST /artifacts ----------

type PublishArtifactInput struct {
	Body struct {
		FeatureID            string `json:"feature_id,omitempty" example:"checkout-loyalty-points" doc:"Feature id for feature-backed artifacts. Empty is allowed for standalone quick Context Pack artifacts."`
		RequestType          string `json:"request_type" required:"true" enum:"new_feature,change_request,bugfix,unknown"`
		ImpactLevel          string `json:"impact_level" required:"true" enum:"low,medium,high"`
		ArtifactPhase        string `json:"artifact_phase,omitempty" enum:"phase1,phase2" default:"phase1"`
		ArtifactCompleteness string `json:"artifact_completeness,omitempty" enum:"partial,full" default:"partial"`
		Version              string `json:"version" required:"true" pattern:"^v\\d+\\.\\d+$" example:"v0.3"`
		// No pattern constraint: an empty string must fall through to the
		// auto-bump path, not 422. compareVersion tolerates unparseable input
		// (a malformed base simply reads as stale → 409), so the format is
		// documented rather than schema-enforced.
		BaseVersion       string             `json:"base_version,omitempty" example:"v0.2" doc:"Optional optimistic lock: the version this publish was derived from. When provided and no longer the feature's latest, the publish is rejected with 409 instead of silently overwriting. Empty skips the check."`
		Status            string             `json:"status,omitempty" enum:"draft" default:"draft"`
		ConfidenceScore   *float64           `json:"confidence_score,omitempty" minimum:"0" maximum:"1"`
		AmbiguityScore    *float64           `json:"ambiguity_score,omitempty" minimum:"0" maximum:"1"`
		GovernanceVersion string             `json:"governance_version,omitempty"`
		ImpactedServices  []string           `json:"impacted_services" required:"true"`
		ImpactedApps      []string           `json:"impacted_apps,omitempty"`
		Documents         []DocumentInputDTO `json:"documents,omitempty" doc:"Open document list (new API). Takes precedence over files/file_refs when both provided."`
		Files             map[string]string  `json:"files,omitempty" doc:"Fixed-key shape: file_key → base64 content; each key maps to a canonical {path, role} server-side"`
		FileRefs          map[string]string  `json:"file_refs,omitempty" doc:"Fixed-key shape: file_key → committed governance file id"`
		// SP0 governance envelope fields.
		SourceKind     string `json:"source_kind,omitempty" doc:"Origin system kind, e.g. governance, external_tool"`
		SourceID       string `json:"source_id,omitempty" doc:"Origin system artifact/issue id"`
		SourceRevision string `json:"source_revision,omitempty" doc:"VCS revision or version at publish time"`
		Authority      string `json:"authority,omitempty" doc:"Authoritative team or system"`
		GatesProfile   string `json:"gates_profile,omitempty" doc:"Gates workflow profile identifier"`
	}
}

type PublishArtifactOutput struct {
	Body ArtifactDTO
}

// ---------- GET /artifacts ----------

type ListArtifactsInput struct {
	FeatureID string `query:"feature_id"`
	Service   string `query:"service"`
	Status    string `query:"status" enum:"draft,needs_changes,approved,superseded"`
	Limit     int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
	Offset    int    `query:"offset" minimum:"0" default:"0"`
}

type ListArtifactsOutput struct {
	Body struct {
		Items []ArtifactDTO `json:"items"`
		Total int           `json:"total" doc:"Absolute count of artifacts matching the filter, ignoring limit/offset."`
	}
}

// ---------- GET /artifacts/{id} ----------

type GetArtifactInput struct {
	ID string `path:"id" doc:"Artifact UUID"`
}

type GetArtifactOutput struct {
	Body ArtifactDTO
}

// ---------- DELETE /artifacts/{id} ----------

type DeleteArtifactInput struct {
	ID string `path:"id" doc:"Artifact UUID"`
}

type DeleteArtifactOutput struct {
	Body struct {
		OK bool `json:"ok"`
	} `json:"body"`
}

// ---------- PATCH /artifacts/{id}/status ----------

type UpdateStatusInput struct {
	ID   string `path:"id"`
	Body struct {
		Status       string `json:"status" required:"true" enum:"approved,needs_changes,superseded"`
		ApprovedBy   string `json:"approved_by,omitempty"`
		ReviewRating int    `json:"review_rating,omitempty" minimum:"1" maximum:"4" doc:"1=unusable, 4=ready"`
		Note         string `json:"note,omitempty"`
		Manifest     string `json:"manifest,omitempty" doc:"Updated manifest.json content to keep stored artifact metadata in sync with the promoted status"`
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
	Path      string `json:"path"`
	Role      string `json:"role"`
	SizeBytes int64  `json:"size_bytes"`
}

type ListArtifactFilesInput struct {
	ID   string `path:"id"`
	Role string `query:"role" doc:"Optional: filter by role"`
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
	State        string    `json:"state" enum:"pass,warn,fail,needs_human_review,not_applicable"`
	Hint         string    `json:"hint"`
	EvidenceJSON string    `json:"evidence_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type ListArtifactReadinessRunsInput struct {
	ID    string `path:"id"`
	Limit int    `query:"limit"`
}

type ListArtifactReadinessRunsOutput struct {
	Body struct {
		Items []ArtifactReadinessRunDTO `json:"items"`
	}
}

type RefreshArtifactReadinessRunsInput struct {
	ID   string `path:"id"`
	Body *struct {
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

// ---------- GET /artifacts/{id}/files/{key} ----------

// GetArtifactFileInput accepts a document path via query param so slash-containing
// paths (e.g. docs/proposal.md) are not mangled by URL routing.
// The {key} path param selects a document by fixed file key.
type GetArtifactFileInput struct {
	ID   string `path:"id"`
	Key  string `path:"key" doc:"Fixed file key (e.g. prd, spec); use the path query param for open document paths"`
	Path string `query:"path" doc:"Document path within the artifact (takes precedence over {key} when provided)"`
}

type GetArtifactFileOutput struct {
	Body struct {
		SignedURL string    `json:"signed_url"`
		ExpiresAt time.Time `json:"expires_at"`
		SizeBytes int64     `json:"size_bytes"`
		Content   string    `json:"content,omitempty" doc:"Inline valid UTF-8 content fallback for browser clients that cannot fetch the presigned URL; omitted for files larger than 1 MiB"`
	}
}

// ---------- GET /conflicts ----------

type CheckConflictsInput struct {
	Services           []string `query:"services" required:"true" doc:"Comma- or repeated-parameter list of impacted services"`
	CandidateFeatureID string   `query:"candidate_feature_id" doc:"Feature ID of the candidate being checked (populates feature_a in response)"`
	CandidateVersion   string   `query:"candidate_version" doc:"Version of the candidate being checked"`
	CandidateStatus    string   `query:"candidate_status" doc:"Status of the candidate being checked (default: draft)"`
}

type ConflictFeatureRefDTO struct {
	FeatureID string `json:"feature_id"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	// ManifestURL is reserved in the response schema per spec §6.4 but is not
	// populated today — presigning the manifest for every overlap hit would add
	// S3 latency to the hot conflict-check path. Revisit alongside artifact
	// manifest publishing work; emit only on demand.
	ManifestURL string `json:"manifest_url,omitempty"`
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
	EventType  string    `query:"event_type"`
	ArtifactID string    `query:"artifact_id" doc:"Restrict events to one artifact"`
	After      time.Time `query:"after" doc:"Return events with created_at > after"`
	Limit      int       `query:"limit" minimum:"1" maximum:"500" default:"100"`
}

type EventDTO struct {
	ID         string         `json:"id"`
	ArtifactID string         `json:"artifact_id"`
	EventType  string         `json:"event_type" enum:"artifact.published,artifact.needs_changes,artifact.superseded,feature.canonical_changed,change_request.acceptance_criteria_changed"`
	Payload    map[string]any `json:"payload"`
	CreatedAt  time.Time      `json:"created_at"`
}

type ListEventsOutput struct {
	Body struct {
		Items []EventDTO `json:"items"`
	}
}

// ---------- Artifact IDE (session + revision surface) ----------

type ArtifactEditSessionDTO struct {
	ID              string    `json:"id"`
	BaseArtifactID  string    `json:"base_artifact_id"`
	BaseVersion     string    `json:"base_version,omitempty"`
	BaseRevisionID  string    `json:"base_revision_id,omitempty"`
	State           string    `json:"state" enum:"active,saved,discarded,stale_base,expired"`
	SavedRevisionID string    `json:"saved_revision_id,omitempty"`
	LastDiffSummary string    `json:"last_diff_summary,omitempty"`
	SourceKind      string    `json:"source_kind,omitempty"`
	SourceID        string    `json:"source_id,omitempty"`
	CompareToken    string    `json:"compare_token,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

type ArtifactEditSessionFileDTO struct {
	Key      string `json:"key"`
	Content  string `json:"content,omitempty"`
	Modified bool   `json:"modified"`
}

type ArtifactEditDiffFileDTO struct {
	Key      string                    `json:"key"`
	Status   string                    `json:"status"`
	Modified bool                      `json:"modified"`
	Hunks    []ArtifactEditDiffHunkDTO `json:"hunks,omitempty"`
}

type ArtifactEditDiffHunkDTO struct {
	ID    string `json:"id"`
	State string `json:"state" enum:"pending,applied,rejected"`
	// FileKey, BaseText, and WorkingText carry the hunk's content so a reviewer
	// can render and decide each hunk without re-deriving it from the file-level
	// unified_diff. Granularity is currently one hunk per modified file.
	FileKey     string `json:"file_key,omitempty"`
	BaseText    string `json:"base_text,omitempty"`
	WorkingText string `json:"working_text,omitempty"`
}

type ArtifactSavedRevisionDTO struct {
	RevisionID     string `json:"revision_id"`
	BaseArtifactID string `json:"base_artifact_id"`
	ArtifactID     string `json:"artifact_id,omitempty"`
	// MaterializedArtifactID is the ID of the new draft artifact created from
	// the effective working files when a reconciliation proposal is approved.
	// Empty for non-proposal sessions (per spec §1).
	MaterializedArtifactID string    `json:"materialized_artifact_id,omitempty"`
	State                  string    `json:"state" enum:"saved,stale_base,stale_parent"`
	SessionID              string    `json:"session_id,omitempty"`
	ParentRevisionID       string    `json:"parent_revision_id,omitempty"`
	LineageRootArtifactID  string    `json:"lineage_root_artifact_id,omitempty"`
	CreatedAt              time.Time `json:"created_at,omitempty"`
}

type CreateArtifactEditSessionInput struct {
	Body struct {
		ArtifactID     string `json:"artifact_id,omitempty"`
		BaseArtifactID string `json:"base_artifact_id,omitempty"`
		BaseRevisionID string `json:"base_revision_id,omitempty"`
		RequestedBy    string `json:"requested_by,omitempty"`
		// SourceKind/SourceID tag the session as a reconciliation proposal and
		// link it to its origin (e.g. "feedback_event" + the event id). Set by
		// the agent when it drafts a proposed artifact update from a signal.
		SourceKind string `json:"source_kind,omitempty"`
		SourceID   string `json:"source_id,omitempty"`
		// WorkingFiles seeds resolved working content at creation. Each entry
		// overrides the working side of one file; omitted keys default to the
		// base content. A client re-applying edits onto a moved base creates the
		// session already-resolved in this one request, so there is no
		// create-then-write window in which an abandoned session can orphan.
		WorkingFiles []ArtifactEditWorkingFileInput `json:"working_files,omitempty"`
	}
}

// ArtifactEditWorkingFileInput is one resolved working-file override supplied at
// session creation. Per spec §14 the session row and its file rows are written
// in one transaction, so the seeded session is atomic.
type ArtifactEditWorkingFileInput struct {
	Key     string `json:"key" required:"true"`
	Content string `json:"content"`
}

type ListArtifactEditProposalsInput struct {
	_ struct{} `json:"-"`
}

type ListArtifactEditProposalsOutput struct {
	Body struct {
		Items []ArtifactEditSessionDTO `json:"items"`
	}
}

type CreateArtifactEditSessionOutput struct {
	Body ArtifactEditSessionDTO
}

type GetArtifactEditSessionInput struct {
	ID string `path:"id"`
}

type GetArtifactEditSessionOutput struct {
	Body ArtifactEditSessionDTO
}

type DeleteArtifactEditSessionInput struct {
	ID string `path:"id"`
}

type DeleteArtifactEditSessionOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type ListArtifactEditSessionFilesInput struct {
	ID string `path:"id"`
}

type ListArtifactEditSessionFilesOutput struct {
	Body struct {
		Items []ArtifactEditSessionFileDTO `json:"items"`
	}
}

type GetArtifactEditSessionFileInput struct {
	ID  string `path:"id"`
	Key string `path:"key"`
}

type GetArtifactEditSessionFileOutput struct {
	Body ArtifactEditSessionFileDTO
}

type PatchArtifactEditSessionInput struct {
	ID   string `path:"id"`
	Body struct {
		Key         string           `json:"key,omitempty"`
		FileKey     string           `json:"file_key,omitempty"`
		Patch       string           `json:"patch,omitempty"`
		Operations  []map[string]any `json:"operations,omitempty"`
		Selection   map[string]any   `json:"selection,omitempty"`
		Instruction string           `json:"instruction,omitempty"`
		HunkID      string           `json:"hunk_id,omitempty"`
		HunkState   string           `json:"hunk_state,omitempty" enum:"pending,applied,rejected"`
		// DecidedBy records the actor for a hunk decision. Doc Registry has no
		// HTTP auth, so the actor arrives in the request body.
		DecidedBy string `json:"decided_by,omitempty"`
	}
}

type PatchArtifactEditSessionOutput struct {
	Body ArtifactEditSessionFileDTO
}

type ReplaceArtifactEditSessionFileInput struct {
	ID   string `path:"id"`
	Key  string `path:"key"`
	Body struct {
		Content string `json:"content" required:"true"`
	}
}

type ReplaceArtifactEditSessionFileOutput struct {
	Body ArtifactEditSessionFileDTO
}

type GetArtifactEditSessionDiffInput struct {
	ID string `path:"id"`
}

type GetArtifactEditSessionDiffOutput struct {
	Body struct {
		Summary     string                    `json:"summary"`
		Files       []ArtifactEditDiffFileDTO `json:"files,omitempty"`
		UnifiedDiff string                    `json:"unified_diff,omitempty"`
	}
}

type SaveArtifactEditSessionInput struct {
	ID   string `path:"id"`
	Body struct {
		Summary      string `json:"summary,omitempty"`
		RequestedBy  string `json:"requested_by,omitempty"`
		CompareToken string `json:"compare_token,omitempty"`
	}
}

type SaveArtifactEditSessionOutput struct {
	Body ArtifactSavedRevisionDTO
}

type GetArtifactSavedRevisionInput struct {
	RevisionID string `path:"revision_id"`
}

type GetArtifactSavedRevisionOutput struct {
	Body ArtifactSavedRevisionDTO
}

type GetArtifactSavedRevisionDiffInput struct {
	RevisionID string `path:"revision_id"`
}

type GetArtifactSavedRevisionDiffOutput struct {
	Body struct {
		Summary     string                    `json:"summary"`
		Files       []ArtifactEditDiffFileDTO `json:"files,omitempty"`
		UnifiedDiff string                    `json:"unified_diff,omitempty"`
	}
}

type ListArtifactRevisionsInput struct {
	ID string `path:"id"`
}

type ListArtifactRevisionsOutput struct {
	Body struct {
		Items []ArtifactSavedRevisionDTO `json:"items"`
	}
}
