package api

import "github.com/specgate/doc-registry/internal/workboard"

type WorkBoardIDInput struct {
	ID string `path:"id"`
}

type DeleteWorkBoardOutput struct{}

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
		Key  string `json:"key" required:"true"`
		Name string `json:"name,omitempty"`
	}
}

type PatchFeatureInput struct {
	ID   string `path:"id"`
	Body workboard.Feature
}

type FeatureOutput struct {
	Body workboard.Feature
}

type SetFeatureSummaryInput struct {
	ID   string `path:"id"`
	Body struct {
		SummaryMD     string `json:"summary_md" required:"true"`
		SourceVersion string `json:"source_version,omitempty"`
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
	ID   string `path:"id"`
	Body workboard.ChangeRequest
}

type ChangeRequestOutput struct {
	Body workboard.ChangeRequest
}

type UnarchiveChangeRequestInput struct {
	ID   string `path:"id"`
	Body struct {
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
	ID    string `path:"id"`
	Limit int    `query:"limit"`
}

type ListGateRunsOutput struct {
	Body struct {
		Items []workboard.GateRun `json:"items"`
	}
}

type RefreshGateRunsInput struct {
	ID   string `path:"id"`
	Body *struct {
		Evaluations []workboard.GateEvaluation `json:"evaluations,omitempty"`
	} `json:"body,omitempty"`
}

type PatchArtifactPointerInput struct {
	ID   string `path:"id"`
	Body struct {
		ArtifactID string `json:"artifact_id" required:"true"`
	}
}

type ListStaleWarningsInput struct {
	FeatureID       string `query:"feature_id"`
	ChangeRequestID string `query:"change_request_id"`
}

type ListStaleWarningsOutput struct {
	Body struct {
		Items []workboard.StaleWarning `json:"items"`
	}
}
