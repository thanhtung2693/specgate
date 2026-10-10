// Package provenance projects repository metadata without transport credentials.
package provenance

import (
	"maps"
	"net/url"
	"strings"
	"unicode"
)

// Repository retains supported repository coordinates, not authentication or
// URL query/fragment data. Opaque remote helpers cannot be safely projected.
func Repository(origin string) string {
	origin = strings.TrimSpace(origin)
	if strings.IndexFunc(origin, unicode.IsControl) >= 0 {
		return ""
	}
	// Git treats a slash before the first colon as a local path, not a URL
	// or remote helper. Preserve literal names containing colons there.
	if colon := strings.IndexByte(origin, ':'); colon >= 0 && strings.ContainsAny(origin[:colon], `/\`) {
		return origin
	}
	if strings.Contains(origin, "://") {
		u, err := url.Parse(origin)
		if err != nil || u.Opaque != "" {
			return ""
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "git", "ftp", "ftps", "ssh", "git+ssh", "ssh+git":
			if u.Hostname() == "" {
				return ""
			}
		case "file":
		default:
			return ""
		}
		ssh := strings.EqualFold(u.Scheme, "ssh") || strings.EqualFold(u.Scheme, "git+ssh") || strings.EqualFold(u.Scheme, "ssh+git")
		if ssh && u.User != nil {
			u.User = url.User(u.User.Username())
		} else {
			u.User = nil
		}
		u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
		u.ForceQuery = false
		return u.String()
	}
	if colon := strings.IndexByte(origin, ':'); colon >= 0 {
		switch strings.ToLower(origin[:colon]) {
		case "http", "https", "git", "ftp", "ftps", "ssh", "git+ssh", "ssh+git", "file":
			return ""
		}
		// Windows drive paths and scp IPv6 hosts are not remote-helper syntax.
		if colon+1 < len(origin) && origin[colon+1] == ':' && !strings.Contains(origin[:colon], "[") {
			return ""
		}
	}
	return origin
}

// Receipts copies just the known receipt locations. It does not rewrite stored
// history, opaque diff digests, unrelated evidence or peer completion IDs.
func Receipts(body map[string]any) map[string]any {
	out := maps.Clone(body)
	redact := func(raw any) any {
		receipt, ok := raw.(map[string]any)
		if !ok {
			return raw
		}
		copy := maps.Clone(receipt)
		if raw, exists := receipt["repository"]; exists {
			origin, _ := raw.(string)
			copy["repository"] = Repository(origin)
		}
		return copy
	}
	if receipt, ok := out["git_receipt"]; ok {
		out["git_receipt"] = redact(receipt)
	}
	if bound, ok := out["peer_review_of"].(map[string]any); ok {
		copy := maps.Clone(bound)
		if receipt, exists := bound["git_receipt"]; exists {
			copy["git_receipt"] = redact(receipt)
		}
		out["peer_review_of"] = copy
	}
	return out
}
