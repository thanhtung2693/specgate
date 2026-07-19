package s3

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// GovernanceUploadObjectKey returns s3://{bucket}/{prefix}governance/resources/uploads/{uuid}{ext}.
// The optional prefix (e.g. "doc-registry/") namespaces uploads when the bucket is shared.
func GovernanceUploadObjectKey(prefix, contentType string) string {
	return prefix + fmt.Sprintf("governance/resources/uploads/%s%s", uuid.New().String(), extForGovernanceContentType(contentType))
}

func extForGovernanceContentType(ct string) string {
	c := strings.ToLower(strings.TrimSpace(ct))
	switch c {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	case "text/markdown", "text/x-markdown":
		return ".md"
	case "text/plain":
		return ".txt"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/ogg", "audio/x-ogg":
		return ".ogg"
	case "audio/webm":
		return ".webm"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/aac":
		return ".aac"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	default:
		return ".bin"
	}
}
