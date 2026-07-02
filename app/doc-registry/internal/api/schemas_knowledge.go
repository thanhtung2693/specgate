package api

import (
	"mime/multipart"
	"time"
)

// ---------- Governance Knowledge ----------

type KnowledgeDocumentDTO struct {
	DocumentID       string    `json:"document_id"`
	Version          string    `json:"version"`
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
	DocumentID string `path:"document_id"`
	Version    string `query:"version"`
}

type GetKnowledgeDocumentOutput struct {
	Body struct {
		Document         KnowledgeDocumentDTO   `json:"document"`
		History          []KnowledgeDocumentDTO `json:"history"`
		ExtractedPreview string                 `json:"extracted_preview,omitempty"`
	}
}

type DeleteKnowledgeDocumentInput struct {
	DocumentID string `path:"document_id"`
	Version    string `query:"version"`
}

type DeleteKnowledgeDocumentOutput struct {
	Body struct {
		Deleted bool `json:"deleted"`
	}
}

type CreateKnowledgeVersionInput struct {
	DocumentID string `path:"document_id"`
	Body       struct {
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

type GovernanceContextSearchInput struct {
	Body struct {
		Query           string   `json:"query" required:"true"`
		LinkedFeatureID string   `json:"linked_feature_id,omitempty"`
		LinkedRequestID string   `json:"linked_request_id,omitempty"`
		DocumentTypes   []string `json:"document_types,omitempty"`
		AuthorityLevels []string `json:"authority_levels,omitempty"`
		MaxChunks       int      `json:"max_chunks,omitempty" minimum:"1" maximum:"50"`
		IncludeHistory  bool     `json:"include_history,omitempty"`
	}
}

type GovernanceContextSearchOutput struct {
	Body struct {
		Results []GovernanceContextResultDTO `json:"results"`
	}
}

type GovernanceContextResultDTO struct {
	DocumentID     string  `json:"document_id"`
	Version        string  `json:"version"`
	Title          string  `json:"title"`
	DocumentType   string  `json:"document_type"`
	AuthorityLevel string  `json:"authority_level"`
	ChunkText      string  `json:"chunk_text"`
	Score          float64 `json:"score"`
	SourceURI      string  `json:"source_uri"`
	ChunkIndex     int     `json:"chunk_index"`
}
