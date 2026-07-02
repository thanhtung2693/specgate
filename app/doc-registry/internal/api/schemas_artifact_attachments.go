package api

import "time"

// AttachmentDTO is the OpenAPI-facing representation of a feature-scoped
// reference attachment (internal/artifactattachment.Attachment).
type AttachmentDTO struct {
	ID               string    `json:"id"`
	FeatureID        string    `json:"feature_id"`
	Kind             string    `json:"kind" enum:"link,file,image"`
	URL              string    `json:"url,omitempty"`
	GovernanceFileID string    `json:"governance_file_id,omitempty"`
	Title            string    `json:"title,omitempty"`
	Note             string    `json:"note,omitempty"`
	Audience         string    `json:"audience" enum:"gate,coding_agent,both"`
	CreatedBy        string    `json:"created_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// ---------- POST /features/{id}/attachments ----------

type CreateAttachmentInput struct {
	ID   string `path:"id" doc:"Feature id the attachment is scoped to"`
	Body struct {
		Kind             string `json:"kind" required:"true" enum:"link,file,image"`
		URL              string `json:"url,omitempty" doc:"External URL (required for kind=link)"`
		GovernanceFileID string `json:"governance_file_id,omitempty" doc:"governance_files id (required for kind=file/image)"`
		Title            string `json:"title,omitempty"`
		Note             string `json:"note,omitempty"`
		Audience         string `json:"audience,omitempty" enum:"gate,coding_agent,both" default:"gate" doc:"Who consumes it: gate (reviewer), coding_agent (handoff), or both. Defaults to gate-only."`
		CreatedBy        string `json:"created_by,omitempty"`
	}
}

type CreateAttachmentOutput struct {
	Body AttachmentDTO
}

// ---------- GET /features/{id}/attachments ----------

type ListAttachmentsInput struct {
	ID string `path:"id" doc:"Feature id"`
}

type ListAttachmentsOutput struct {
	Body struct {
		Items []AttachmentDTO `json:"items"`
	}
}

// ---------- DELETE /attachments/{id} ----------

type DeleteAttachmentInput struct {
	ID string `path:"id" doc:"Attachment id"`
}

type DeleteAttachmentOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}
