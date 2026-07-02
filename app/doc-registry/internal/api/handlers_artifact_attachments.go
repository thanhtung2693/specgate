package api

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/governancefiles"
)

// isHTTPURL reports whether s is an absolute http(s) URL. Rejecting other schemes
// (javascript:, data:, file:, …) at creation keeps a stored attachment from
// becoming a script-injection vector when rendered as a link in the UI.
func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// CreateFeatureAttachment pins a reference attachment (link / file / image) to a
// feature. Kind decides the required payload: link needs a url; file/image need
// a governance_file_id. Audience defaults to gate-only — reaching the coding agent
// is a deliberate opt-in.
func (h *Handlers) CreateFeatureAttachment(
	ctx context.Context,
	in *CreateAttachmentInput,
) (*CreateAttachmentOutput, error) {
	if h.ArtifactAttachments == nil {
		return nil, notImplemented("create_feature_attachment")
	}
	featureID := strings.TrimSpace(in.ID)
	if featureID == "" {
		return nil, huma.Error400BadRequest("feature id is required")
	}

	kind := artifactattachment.Kind(strings.TrimSpace(in.Body.Kind))
	if !artifactattachment.ValidKind(kind) {
		return nil, huma.Error400BadRequest("kind must be link, file, or image")
	}
	linkURL := strings.TrimSpace(in.Body.URL)
	fileID := strings.TrimSpace(in.Body.GovernanceFileID)
	if kind == artifactattachment.KindLink {
		if linkURL == "" {
			return nil, huma.Error400BadRequest("url is required for kind=link")
		}
		if !isHTTPURL(linkURL) {
			return nil, huma.Error400BadRequest("url must be an absolute http(s) URL")
		}
		fileID = "" // a link carries no governance file reference
	} else {
		if fileID == "" {
			return nil, huma.Error400BadRequest("governance_file_id is required for kind=file/image")
		}
		// Reject a dangling reference: the governance file must exist and be ready,
		// otherwise the attachment 404s on serve. Skip when the store is absent.
		if h.GovernanceFiles != nil {
			if _, err := h.GovernanceFiles.Get(ctx, fileID); err != nil {
				if errors.Is(err, governancefiles.ErrNotFound) {
					return nil, huma.Error400BadRequest("governance_file_id does not reference a ready file")
				}
				return nil, huma.Error500InternalServerError("verify governance file", err)
			}
		}
		linkURL = "" // a file/image carries no external url
	}

	audience := artifactattachment.Audience(strings.TrimSpace(in.Body.Audience))
	if audience == "" {
		audience = artifactattachment.AudienceGate
	}
	if !artifactattachment.ValidAudience(audience) {
		return nil, huma.Error400BadRequest("audience must be gate, coding_agent, or both")
	}

	created, err := h.ArtifactAttachments.Create(ctx, artifactattachment.Attachment{
		ID:               uuid.NewString(),
		FeatureID:        featureID,
		Kind:             kind,
		URL:              linkURL,
		GovernanceFileID: fileID,
		Title:            strings.TrimSpace(in.Body.Title),
		Note:             strings.TrimSpace(in.Body.Note),
		Audience:         audience,
		CreatedBy:        strings.TrimSpace(in.Body.CreatedBy),
		CreatedAt:        time.Now().UTC(),
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("create attachment", err)
	}
	return &CreateAttachmentOutput{Body: attachmentDTO(created)}, nil
}

// ListFeatureAttachments returns a feature's reference attachments, newest first.
func (h *Handlers) ListFeatureAttachments(
	ctx context.Context,
	in *ListAttachmentsInput,
) (*ListAttachmentsOutput, error) {
	if h.ArtifactAttachments == nil {
		return nil, notImplemented("list_feature_attachments")
	}
	items, err := h.ArtifactAttachments.ListByFeature(ctx, strings.TrimSpace(in.ID))
	if err != nil {
		return nil, huma.Error500InternalServerError("list attachments", err)
	}
	out := &ListAttachmentsOutput{}
	out.Body.Items = make([]AttachmentDTO, 0, len(items))
	for i := range items {
		out.Body.Items = append(out.Body.Items, attachmentDTO(&items[i]))
	}
	return out, nil
}

// DeleteFeatureAttachment removes a single attachment by id.
func (h *Handlers) DeleteFeatureAttachment(
	ctx context.Context,
	in *DeleteAttachmentInput,
) (*DeleteAttachmentOutput, error) {
	if h.ArtifactAttachments == nil {
		return nil, notImplemented("delete_feature_attachment")
	}
	if err := h.ArtifactAttachments.Delete(ctx, strings.TrimSpace(in.ID)); err != nil {
		if errors.Is(err, artifactattachment.ErrNotFound) {
			return nil, huma.Error404NotFound("attachment not found")
		}
		return nil, huma.Error500InternalServerError("delete attachment", err)
	}
	out := &DeleteAttachmentOutput{}
	out.Body.OK = true
	return out, nil
}

func attachmentDTO(a *artifactattachment.Attachment) AttachmentDTO {
	return AttachmentDTO{
		ID:               a.ID,
		FeatureID:        a.FeatureID,
		Kind:             string(a.Kind),
		URL:              a.URL,
		GovernanceFileID: a.GovernanceFileID,
		Title:            a.Title,
		Note:             a.Note,
		Audience:         string(a.Audience),
		CreatedBy:        a.CreatedBy,
		CreatedAt:        a.CreatedAt.UTC(),
	}
}
