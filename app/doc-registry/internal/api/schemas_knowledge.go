package api

import (
	"mime/multipart"
	"time"
)

// ---------- Governance Knowledge ----------

type KnowledgeDocumentDTO struct {
	DocumentID       string    `json:"document_id"`
	Version          string    `json:"version"`
	WorkspaceID      string    `json:"workspace_id,omitempty"`
	ParentVersion    string    `json:"parent_version,omitempty"`
	IsLatest         bool      `json:"is_latest"`
	Title            string    `json:"title"`
	DocumentType     string    `json:"document_type" enum:"product_brief,srs,design_reference,supporting_doc,existing_artifact,qa_finding,policy_doc"`
	AuthorityLevel   string    `json:"authority_level" enum:"source_of_truth,high,reference,low"`
	SourceKind       string    `json:"source_kind" enum:"upload,text"`
	SourceURI        string    `json:"source_uri,omitempty"`
	MimeType         string    `json:"mime_type,omitempty"`
	OriginalFilename string    `json:"original_filename,omitempty"`
	Status           string    `json:"status" enum:"uploaded,parsing,chunked,embedded,indexed,failed"`
	LinkedFeatureID  string    `json:"linked_feature_id,omitempty"`
	LinkedRequestID  string    `json:"linked_request_id,omitempty"`
	UploadedBy       string    `json:"uploaded_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Summary          string    `json:"summary,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	Tags             []string  `json:"tags,omitempty"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	ChunkCount       int       `json:"chunk_count,omitempty"`
}

type CreateTextDocumentInput struct {
	Body struct {
		WorkspaceID     string   `json:"workspace_id" required:"true"`
		DocumentID      string   `json:"document_id,omitempty"`
		ParentVersion   string   `json:"parent_version,omitempty"`
		NewVersion      string   `json:"new_version,omitempty"`
		Title           string   `json:"title" required:"true"`
		DocumentType    string   `json:"document_type" required:"true" enum:"product_brief,srs,design_reference,supporting_doc,existing_artifact,qa_finding,policy_doc"`
		AuthorityLevel  string   `json:"authority_level" required:"true" enum:"source_of_truth,high,reference,low"`
		LinkedFeatureID string   `json:"linked_feature_id,omitempty"`
		LinkedRequestID string   `json:"linked_request_id,omitempty"`
		UploadedBy      string   `json:"uploaded_by,omitempty"`
		ActorRole       string   `json:"actor_role,omitempty"`
		Tags            []string `json:"tags,omitempty"`
		Notes           string   `json:"notes,omitempty"`
		Content         string   `json:"content" required:"true"`
	}
}

type CreateUploadDocumentInput struct {
	RawBody multipart.Form
}

type KnowledgeDocumentOutput struct {
	Body KnowledgeDocumentDTO
}

type ListKnowledgeDocumentsInput struct {
	WorkspaceID     string `query:"workspace_id" required:"true"`
	LinkedFeatureID string `query:"linked_feature_id"`
	LinkedRequestID string `query:"linked_request_id"`
	IncludeHistory  bool   `query:"include_history" default:"false"`
	DocumentType    string `query:"document_type" enum:"product_brief,srs,design_reference,supporting_doc,existing_artifact,qa_finding,policy_doc"`
	Status          string `query:"status" enum:"uploaded,parsing,chunked,embedded,indexed,failed"`
	Limit           int    `query:"limit" minimum:"1" maximum:"200" default:"100"`
	Offset          int    `query:"offset" minimum:"0" default:"0"`
}

type ListKnowledgeDocumentsOutput struct {
	Body struct {
		Items []KnowledgeDocumentDTO `json:"items"`
		Total int                    `json:"total"`
		// EmbeddingsEnabled is false when no embedding provider is configured
		// (no GOOGLE_API_KEY / GEMINI_API_KEY). The UI warns and disables upload.
		EmbeddingsEnabled bool `json:"embeddings_enabled"`
	}
}

type GetKnowledgeDocumentInput struct {
	DocumentID  string `path:"document_id"`
	Version     string `query:"version"`
	WorkspaceID string `query:"workspace_id" required:"true"`
}

type GetKnowledgeDocumentOutput struct {
	Body struct {
		Document         KnowledgeDocumentDTO   `json:"document"`
		History          []KnowledgeDocumentDTO `json:"history"`
		ExtractedPreview string                 `json:"extracted_preview,omitempty"`
	}
}

type RetryKnowledgeDocumentInput struct {
	DocumentID  string `path:"document_id"`
	Version     string `query:"version"`
	WorkspaceID string `query:"workspace_id" required:"true"`
}

type DeleteKnowledgeDocumentInput struct {
	DocumentID  string `path:"document_id"`
	Version     string `query:"version"`
	WorkspaceID string `query:"workspace_id" required:"true"`
}

type DeleteKnowledgeDocumentOutput struct {
	Body struct {
		Deleted bool `json:"deleted"`
	}
}

type CreateKnowledgeVersionInput struct {
	DocumentID string `path:"document_id"`
	Body       struct {
		WorkspaceID     string   `json:"workspace_id" required:"true"`
		ParentVersion   string   `json:"parent_version,omitempty"`
		NewVersion      string   `json:"new_version,omitempty"`
		Title           string   `json:"title" required:"true"`
		DocumentType    string   `json:"document_type" required:"true" enum:"product_brief,srs,design_reference,supporting_doc,existing_artifact,qa_finding,policy_doc"`
		AuthorityLevel  string   `json:"authority_level" required:"true" enum:"source_of_truth,high,reference,low"`
		LinkedFeatureID string   `json:"linked_feature_id,omitempty"`
		LinkedRequestID string   `json:"linked_request_id,omitempty"`
		UploadedBy      string   `json:"uploaded_by,omitempty"`
		ActorRole       string   `json:"actor_role,omitempty"`
		Tags            []string `json:"tags,omitempty"`
		Notes           string   `json:"notes,omitempty"`
		Content         string   `json:"content" required:"true"`
	}
}

type CurateKnowledgeLinksInput struct {
	DocumentID string `path:"document_id"`
	Body       struct {
		WorkspaceID      string `json:"workspace_id" required:"true"`
		Version          string `json:"version,omitempty"`
		LinkedFeatureID  string `json:"linked_feature_id,omitempty"`
		LinkedRequestID  string `json:"linked_request_id,omitempty"`
		ClearFeatureLink bool   `json:"clear_feature_link,omitempty"`
		ClearRequestLink bool   `json:"clear_request_link,omitempty"`
		UploadedBy       string `json:"uploaded_by,omitempty"`
		ActorRole        string `json:"actor_role,omitempty"`
		Notes            string `json:"notes,omitempty"`
	}
}

type GovernanceContextSearchInput struct {
	Body struct {
		WorkspaceID     string   `json:"workspace_id" required:"true"`
		Query           string   `json:"query" required:"true"`
		LinkedFeatureID string   `json:"linked_feature_id,omitempty"`
		LinkedRequestID string   `json:"linked_request_id,omitempty"`
		DocumentTypes   []string `json:"document_types,omitempty"`
		AuthorityLevels []string `json:"authority_levels,omitempty"`
		MaxChunks       int      `json:"max_chunks,omitempty" minimum:"1" maximum:"50"`
		IncludeHistory  bool     `json:"include_history,omitempty"`
		ContextMode     string   `json:"context_mode,omitempty" enum:"chunk,section,document"`
		ContextMaxChars int      `json:"context_max_chars,omitempty" minimum:"256" maximum:"12000"`
	}
}

type GovernanceContextSearchOutput struct {
	Body struct {
		Results []GovernanceContextResultDTO `json:"results"`
	}
}

type GovernanceContextResultDTO struct {
	Kind           string  `json:"kind"`
	WorkspaceID    string  `json:"workspace_id"`
	DocumentID     string  `json:"document_id"`
	Version        string  `json:"version"`
	Title          string  `json:"title"`
	DocumentType   string  `json:"document_type"`
	AuthorityLevel string  `json:"authority_level"`
	ChunkText      string  `json:"chunk_text"`
	Score          float64 `json:"score"`
	SourceURI      string  `json:"source_uri"`
	ChunkIndex     int     `json:"chunk_index"`
	URL            string  `json:"url"`

	ContextText     string   `json:"context_text,omitempty"`
	ContextKind     string   `json:"context_kind,omitempty" enum:"chunk,section,document,document_capped"`
	Heading         string   `json:"heading,omitempty"`
	HeadingPath     []string `json:"heading_path,omitempty"`
	SectionIndex    int      `json:"section_index,omitempty"`
	StartChunkIndex int      `json:"start_chunk_index,omitempty"`
	EndChunkIndex   int      `json:"end_chunk_index,omitempty"`
}
