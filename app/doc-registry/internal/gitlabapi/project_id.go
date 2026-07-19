package gitlabapi

import (
	"net/url"
	"strings"
)

// ProjectAPIPathSegment returns the `:id` segment for GitLab `GET /api/v4/projects/:id`.
// Use numeric id (`123`), plain path (`group/sub/repo`), or an already URL-encoded path
// (`group%2Fsub%2Frepo`). Pre-encoded values must not be passed through PathEscape again.
func ProjectAPIPathSegment(projectID string) string {
	s := strings.TrimSpace(strings.Trim(projectID, "/"))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "%") {
		return s
	}
	return url.PathEscape(s)
}
