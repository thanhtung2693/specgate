package api

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/knowledge"
)

func (h *Handlers) CreateTextDocument(ctx context.Context, in *CreateTextDocumentInput) (*KnowledgeDocumentOutput, error) {
	if h.Knowledge == nil {
		return nil, notImplemented("create_text_document")
	}
	if !h.Knowledge.EmbeddingsEnabled() {
		return nil, huma.Error422UnprocessableEntity("knowledge embeddings are not configured", knowledge.ErrEmbeddingsDisabled)
	}
	doc, err := h.Knowledge.CreateText(ctx, knowledge.CreateTextInput{
		Metadata: knowledgeMetadata(
			in.Body.DocumentID,
			in.Body.ParentVersion,
			in.Body.NewVersion,
			in.Body.Title,
			in.Body.DocumentType,
			in.Body.AuthorityLevel,
			in.Body.LinkedFeatureID,
			in.Body.LinkedRequestID,
			in.Body.UploadedBy,
			in.Body.ActorRole,
			in.Body.Tags,
			in.Body.Notes,
		),
		Content: in.Body.Content,
	})
	if err != nil {
		return nil, mapKnowledgeError("create text document", err)
	}
	return &KnowledgeDocumentOutput{Body: knowledgeDTO(doc, 0)}, nil
}

func (h *Handlers) CreateUploadDocument(ctx context.Context, in *CreateUploadDocumentInput) (*KnowledgeDocumentOutput, error) {
	if h.Knowledge == nil {
		return nil, notImplemented("create_upload_document")
	}
	if !h.Knowledge.EmbeddingsEnabled() {
		return nil, huma.Error422UnprocessableEntity("knowledge embeddings are not configured", knowledge.ErrEmbeddingsDisabled)
	}
	data, err := parseUploadForm(&in.RawBody)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(data.file)
	closeErr := data.file.Close()
	if err != nil {
		return nil, huma.Error400BadRequest("read upload", err)
	}
	if closeErr != nil {
		return nil, huma.Error500InternalServerError("close upload", closeErr)
	}
	doc, err := h.Knowledge.CreateUpload(ctx, knowledge.CreateUploadInput{
		Metadata: knowledgeMetadata(
			data.documentID,
			data.parentVersion,
			data.newVersion,
			data.title,
			data.documentType,
			data.authorityLevel,
			data.linkedFeatureID,
			data.linkedRequestID,
			data.uploadedBy,
			data.actorRole,
			data.tags,
			data.notes,
		),
		Filename: data.filename,
		MimeType: data.contentType,
		Body:     body,
	})
	if err != nil {
		return nil, mapKnowledgeError("upload document", err)
	}
	return &KnowledgeDocumentOutput{Body: knowledgeDTO(doc, 0)}, nil
}

type uploadFormData struct {
	file            io.ReadCloser
	filename        string
	contentType     string
	documentID      string
	parentVersion   string
	newVersion      string
	title           string
	documentType    string
	authorityLevel  string
	linkedFeatureID string
	linkedRequestID string
	uploadedBy      string
	actorRole       string
	tags            []string
	notes           string
}

func parseUploadForm(form *multipart.Form) (*uploadFormData, error) {
	get := func(name string) string {
		if form == nil || form.Value == nil {
			return ""
		}
		v := form.Value[name]
		if len(v) == 0 {
			return ""
		}
		return strings.TrimSpace(v[0])
	}
	files := form.File["file"]
	if len(files) == 0 {
		return nil, huma.Error400BadRequest("file is required")
	}
	fh := files[0]
	f, err := fh.Open()
	if err != nil {
		return nil, huma.Error400BadRequest("open upload", err)
	}
	return &uploadFormData{
		file:            f,
		filename:        fh.Filename,
		contentType:     fh.Header.Get("Content-Type"),
		documentID:      get("document_id"),
		parentVersion:   get("parent_version"),
		newVersion:      get("new_version"),
		title:           get("title"),
		documentType:    get("document_type"),
		authorityLevel:  get("authority_level"),
		linkedFeatureID: get("linked_feature_id"),
		linkedRequestID: get("linked_request_id"),
		uploadedBy:      get("uploaded_by"),
		actorRole:       get("actor_role"),
		tags:            form.Value["tags"],
		notes:           get("notes"),
	}, nil
}

func (h *Handlers) ListKnowledgeDocuments(ctx context.Context, in *ListKnowledgeDocumentsInput) (*ListKnowledgeDocumentsOutput, error) {
	if h.Knowledge == nil {
		return nil, notImplemented("list_documents")
	}
	items, err := h.Knowledge.List(ctx, knowledge.ListFilter{
		LinkedFeatureID: in.LinkedFeatureID,
		LinkedRequestID: in.LinkedRequestID,
		IncludeHistory:  in.IncludeHistory,
		DocumentType:    knowledge.DocumentType(in.DocumentType),
		Status:          knowledge.Status(in.Status),
		Limit:           in.Limit,
		Offset:          in.Offset,
	})
	if err != nil {
		return nil, mapKnowledgeError("list documents", err)
	}
	out := &ListKnowledgeDocumentsOutput{}
	out.Body.Items = make([]KnowledgeDocumentDTO, 0, len(items))
	for i := range items {
		out.Body.Items = append(out.Body.Items, knowledgeDTO(&items[i], len(items[i].Chunks)))
	}
	out.Body.Total = len(out.Body.Items)
	out.Body.EmbeddingsEnabled = h.Knowledge.EmbeddingsEnabled()
	return out, nil
}

func (h *Handlers) GetKnowledgeDocument(ctx context.Context, in *GetKnowledgeDocumentInput) (*GetKnowledgeDocumentOutput, error) {
	if h.Knowledge == nil {
		return nil, notImplemented("get_document")
	}
	detail, err := h.Knowledge.Detail(ctx, in.DocumentID, in.Version)
	if err != nil {
		return nil, mapKnowledgeError("get document", err)
	}
	out := &GetKnowledgeDocumentOutput{}
	out.Body.Document = knowledgeDTO(&detail.Document, detail.ChunkCount)
	out.Body.History = make([]KnowledgeDocumentDTO, 0, len(detail.History))
	for i := range detail.History {
		out.Body.History = append(out.Body.History, knowledgeDTO(&detail.History[i], 0))
	}
	out.Body.ExtractedPreview = detail.ExtractedPreview
	return out, nil
}

func (h *Handlers) CreateKnowledgeVersion(ctx context.Context, in *CreateKnowledgeVersionInput) (*KnowledgeDocumentOutput, error) {
	documentID := strings.TrimSpace(in.DocumentID)
	if documentID == "" {
		return nil, huma.Error400BadRequest("document_id is required")
	}
	if h.Knowledge == nil {
		return nil, notImplemented("create_document_version")
	}
	doc, err := h.Knowledge.CreateText(ctx, knowledge.CreateTextInput{
		Metadata: knowledgeMetadata(
			documentID,
			in.Body.ParentVersion,
			in.Body.NewVersion,
			in.Body.Title,
			in.Body.DocumentType,
			in.Body.AuthorityLevel,
			in.Body.LinkedFeatureID,
			in.Body.LinkedRequestID,
			in.Body.UploadedBy,
			in.Body.ActorRole,
			in.Body.Tags,
			in.Body.Notes,
		),
		Content: in.Body.Content,
	})
	if err != nil {
		return nil, mapKnowledgeError("create document version", err)
	}
	return &KnowledgeDocumentOutput{Body: knowledgeDTO(doc, 0)}, nil
}

func (h *Handlers) DeleteKnowledgeDocument(ctx context.Context, in *DeleteKnowledgeDocumentInput) (*DeleteKnowledgeDocumentOutput, error) {
	if h.Knowledge == nil {
		return nil, notImplemented("delete_document")
	}
	if err := h.Knowledge.Delete(ctx, in.DocumentID, in.Version); err != nil {
		return nil, mapKnowledgeError("delete document", err)
	}
	out := &DeleteKnowledgeDocumentOutput{}
	out.Body.Deleted = true
	return out, nil
}

func knowledgeMetadata(documentID, parentVersion, newVersion, title, docType, authority, featureID, requestID, uploadedBy, actorRole string, tags []string, notes string) knowledge.Metadata {
	return knowledge.Metadata{
		DocumentID:      documentID,
		ParentVersion:   parentVersion,
		NewVersion:      newVersion,
		Title:           title,
		DocumentType:    knowledge.DocumentType(docType),
		AuthorityLevel:  knowledge.AuthorityLevel(authority),
		LinkedFeatureID: featureID,
		LinkedRequestID: requestID,
		UploadedBy:      uploadedBy,
		ActorRole:       actorRole,
		Tags:            tags,
		Notes:           notes,
	}
}

func knowledgeDTO(doc *knowledge.Document, chunkCount int) KnowledgeDocumentDTO {
	tags := []string{}
	if doc.TagsJSON != "" {
		// Malformed tags JSON is not fatal for a read path; fall back to empty slice.
		_ = json.Unmarshal([]byte(doc.TagsJSON), &tags)
	}
	return KnowledgeDocumentDTO{
		DocumentID:       doc.DocumentID,
		Version:          doc.Version,
		ParentVersion:    doc.ParentVersion,
		IsLatest:         doc.IsLatest,
		Title:            doc.Title,
		DocumentType:     string(doc.DocumentType),
		AuthorityLevel:   string(doc.AuthorityLevel),
		SourceKind:       string(doc.SourceKind),
		SourceURI:        doc.SourceURI,
		MimeType:         doc.MimeType,
		OriginalFilename: doc.OriginalFilename,
		Status:           string(doc.Status),
		LinkedFeatureID:  doc.LinkedFeatureID,
		LinkedRequestID:  doc.LinkedRequestID,
		UploadedBy:       doc.UploadedBy,
		CreatedAt:        doc.CreatedAt,
		UpdatedAt:        doc.UpdatedAt,
		Summary:          doc.Summary,
		Notes:            doc.Notes,
		Tags:             tags,
		ErrorMessage:     doc.ErrorMessage,
		ChunkCount:       chunkCount,
	}
}
