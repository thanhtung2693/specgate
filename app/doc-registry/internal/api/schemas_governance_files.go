package api

// PresignFileInput requests a presigned PUT URL for a new internal governance file.
type PresignFileInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"512"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes" minimum:"0"`
	}
}

type PresignFileOutput struct {
	Body struct {
		FileID    string `json:"file_id"`
		UploadURL string `json:"upload_url"`
		ObjectKey string `json:"object_key"`
	}
}

type GovernanceFileDTO struct {
	FileID     string `json:"file_id"`
	Name       string `json:"name"`
	Mime       string `json:"mime"`
	SizeBytes  int64  `json:"size_bytes"`
	GetURL     string `json:"get_url"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

type CommitFileInput struct {
	ID string `path:"id"`
}

type CommitFileOutput struct {
	Body GovernanceFileDTO
}

type ListFilesInput struct {
	Limit  int    `query:"limit"`
	Offset int    `query:"offset"`
	Q      string `query:"q"`
}

type ListFilesOutput struct {
	Body struct {
		Items []GovernanceFileDTO `json:"items"`
		Total int64               `json:"total"`
	}
}

type TouchFileInput struct {
	ID string `path:"id"`
}

type TouchFileOutput struct {
	Body GovernanceFileDTO
}

type DeleteFileInput struct {
	ID string `path:"id"`
}

type DeleteFileOutput struct{}
