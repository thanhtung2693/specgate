package api

import "github.com/specgate/doc-registry/internal/workboard"

type WorkBoardIDInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
}

type ListFeaturesOutput struct {
	Body struct {
		Items []workboard.Feature `json:"items"`
	}
}

type CreateFeatureInput struct {
	Body workboard.Feature
}

type UpsertFeatureByKeyInput struct {
	Body struct {
		WorkspaceID string `json:"workspace_id,omitempty"`
		Key         string `json:"key" required:"true"`
		Name        string `json:"name,omitempty"`
	}
}

type PatchFeatureInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Body        workboard.Feature
}

type FeatureOutput struct {
	Body workboard.Feature
}

type PromoteArtifactCanonicalInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Body        struct {
		// ApprovedBy records who promoted the artifact (no HTTP auth; supplied in
		// body). Optional; audited on the feature.canonical_changed event.
		ApprovedBy string `json:"approved_by,omitempty"`
	}
}

type ListChangeRequestsOutput struct {
	Body struct {
		Items []workboard.ChangeRequest `json:"items"`
	}
}

type ListChangeRequestsInput struct {
	IncludeArchived bool   `query:"include_archived"`
	WorkspaceID     string `query:"workspace_id"`
}

type CreateChangeRequestInput struct {
	Body workboard.ChangeRequest
}

type PatchChangeRequestInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Body        workboard.ChangeRequest
}

type ChangeRequestOutput struct {
	Body workboard.ChangeRequest
}

type UnarchiveChangeRequestInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Body        struct {
		// Actor that performed the unarchive (no HTTP auth; supplied in body).
		Actor string `json:"actor,omitempty"`
	}
}

type ListAcceptanceCriteriaOutput struct {
	Body struct {
		Items []workboard.AcceptanceCriterion `json:"items"`
	}
}

type NextActionsOutput struct {
	Body struct {
		Items []workboard.NextAction `json:"items"`
	}
}

type ListGateRunsInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Limit       int    `query:"limit" minimum:"1" maximum:"500" default:"50"`
}

type ListGateRunsOutput struct {
	Body struct {
		Items []workboard.GateRun `json:"items"`
	}
}

type RefreshGateRunsInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id"`
	Body        *struct {
		Evaluations     []workboard.GateEvaluation `json:"evaluations,omitempty"`
		EvaluationsOnly bool                       `json:"evaluations_only,omitempty"`
	} `json:"body,omitempty"`
}

type ListStaleWarningsInput struct {
	FeatureID       string `query:"feature_id"`
	ChangeRequestID string `query:"change_request_id"`
	WorkspaceID     string `query:"workspace_id"`
}

type ListStaleWarningsOutput struct {
	Body struct {
		Items []workboard.StaleWarning `json:"items"`
	}
}
