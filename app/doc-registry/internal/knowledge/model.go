package knowledge

import "time"

type DocumentType string

const (
	DocumentTypeProductBrief     DocumentType = "product_brief"
	DocumentTypeSRS              DocumentType = "srs"
	DocumentTypeDesignReference  DocumentType = "design_reference"
	DocumentTypeSupportingDoc    DocumentType = "supporting_doc"
	DocumentTypeExistingArtifact DocumentType = "existing_artifact"
	DocumentTypeQAFinding        DocumentType = "qa_finding"
	DocumentTypePolicyDoc        DocumentType = "policy_doc"
)

type AuthorityLevel string

const (
	AuthoritySourceOfTruth AuthorityLevel = "source_of_truth"
	AuthorityHigh          AuthorityLevel = "high"
	AuthorityReference     AuthorityLevel = "reference"
	AuthorityLow           AuthorityLevel = "low"
)

type SourceKind string

const (
	SourceKindUpload SourceKind = "upload"
	SourceKindText   SourceKind = "text"
)

type Status string

const (
	StatusUploaded Status = "uploaded"
	StatusParsing  Status = "parsing"
	StatusChunked  Status = "chunked"
	StatusEmbedded Status = "embedded"
	StatusIndexed  Status = "indexed"
	StatusFailed   Status = "failed"
)

type Document struct {
	DocumentID       string         `gorm:"column:document_id;primaryKey"`
	Version          string         `gorm:"column:version;primaryKey"`
	WorkspaceID      string         `gorm:"column:workspace_id"`
	ParentVersion    string         `gorm:"column:parent_version"`
	IsLatest         bool           `gorm:"column:is_latest;not null"`
	Title            string         `gorm:"column:title;not null"`
	DocumentType     DocumentType   `gorm:"column:document_type;not null"`
	AuthorityLevel   AuthorityLevel `gorm:"column:authority_level;not null"`
	SourceKind       SourceKind     `gorm:"column:source_kind;not null"`
	SourceURI        string         `gorm:"column:source_uri"`
	MimeType         string         `gorm:"column:mime_type"`
	OriginalFilename string         `gorm:"column:original_filename"`
	Status           Status         `gorm:"column:status;not null"`
	LinkedFeatureID  string         `gorm:"column:linked_feature_id"`
	LinkedRequestID  string         `gorm:"column:linked_request_id"`
	UploadedBy       string         `gorm:"column:uploaded_by"`
	CreatedAt        time.Time      `gorm:"column:created_at;not null"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;not null"`
	Summary          string         `gorm:"column:summary"`
	Notes            string         `gorm:"column:notes"`
	TagsJSON         string         `gorm:"column:tags"`
	ErrorMessage     string         `gorm:"column:error_message"`

	Chunks []Chunk `gorm:"foreignKey:DocumentID,Version;references:DocumentID,Version;constraint:OnDelete:CASCADE"`
	Links  []Link  `gorm:"foreignKey:DocumentID,Version;references:DocumentID,Version;constraint:OnDelete:CASCADE"`
}

func (Document) TableName() string { return "documents" }

type Chunk struct {
	ID              string    `gorm:"column:id;primaryKey"`
	DocumentID      string    `gorm:"column:document_id;not null"`
	Version         string    `gorm:"column:version;not null"`
	ChunkIndex      int       `gorm:"column:chunk_index;not null"`
	ChunkText       string    `gorm:"column:chunk_text;not null"`
	TokenCount      int       `gorm:"column:token_count"`
	Heading         string    `gorm:"column:heading"`
	HeadingPathJSON string    `gorm:"column:heading_path"`
	SectionIndex    int       `gorm:"column:section_index"`
	CreatedAt       time.Time `gorm:"column:created_at;not null"`
}

func (Chunk) TableName() string { return "document_chunks" }

type Link struct {
	ID           string `gorm:"column:id;primaryKey"`
	DocumentID   string `gorm:"column:document_id;not null"`
	Version      string `gorm:"column:version;not null"`
	EntityType   string `gorm:"column:entity_type;not null"`
	EntityID     string `gorm:"column:entity_id;not null"`
	RelationType string `gorm:"column:relation_type;not null"`
}

func (Link) TableName() string { return "document_links" }

type Metadata struct {
	WorkspaceID     string
	Title           string
	DocumentType    DocumentType
	AuthorityLevel  AuthorityLevel
	LinkedFeatureID string
	LinkedRequestID string
	UploadedBy      string
	ActorRole       string
	Tags            []string
	Notes           string
	DocumentID      string
	ParentVersion   string
	NewVersion      string
}

type CreateTextInput struct {
	Metadata
	Content string
}

type CreateUploadInput struct {
	Metadata
	Filename string
	MimeType string
	Body     []byte
}

type CurateLinksInput struct {
	DocumentID       string
	Version          string
	LinkedFeatureID  string
	LinkedRequestID  string
	ClearFeatureLink bool
	ClearRequestLink bool
	UploadedBy       string
	ActorRole        string
	Notes            string
}

type ListFilter struct {
	WorkspaceID     string
	LinkedFeatureID string
	LinkedRequestID string
	DocumentType    DocumentType
	Status          Status
	IncludeHistory  bool
	Limit           int
	Offset          int
}

type Detail struct {
	Document         Document
	History          []Document
	ChunkCount       int
	ExtractedPreview string
}

// CitationValidationInput identifies one exact Knowledge chunk span proposed
// as evidence for a submitted artifact version.
type CitationValidationInput struct {
	WorkspaceID     string
	DocumentID      string
	Version         string
	StartChunkIndex int
	EndChunkIndex   int
	ExcerptDigest   string
}

// Citation is server-normalized evidence for a validated Knowledge span.
type Citation struct {
	WorkspaceID     string
	DocumentID      string
	Version         string
	StartChunkIndex int
	EndChunkIndex   int
	URL             string
	Title           string
	AuthorityLevel  AuthorityLevel
	Stale           bool
}

type ContextMode string

const (
	ContextModeChunk    ContextMode = "chunk"
	ContextModeSection  ContextMode = "section"
	ContextModeDocument ContextMode = "document"
)

type SearchInput struct {
	WorkspaceID     string
	Query           string
	LinkedFeatureID string
	LinkedRequestID string
	DocumentTypes   []DocumentType
	AuthorityLevels []AuthorityLevel
	MaxChunks       int
	IncludeHistory  bool
	ContextMode     ContextMode
	ContextMaxChars int
}

type SearchResult struct {
	WorkspaceID    string
	DocumentID     string
	Version        string
	Title          string
	DocumentType   DocumentType
	AuthorityLevel AuthorityLevel
	ChunkText      string
	Score          float64
	SourceURI      string
	ChunkIndex     int

	// Section-context expansion (Task 10). ContextText is the bounded context
	// for the hit; ContextKind reports what the caller actually got.
	ContextText     string
	ContextKind     string
	Heading         string
	HeadingPath     []string
	SectionIndex    int
	StartChunkIndex int
	EndChunkIndex   int
}
