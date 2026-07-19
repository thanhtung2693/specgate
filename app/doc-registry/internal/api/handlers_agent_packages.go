package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/specgate/doc-registry/internal/agentpackages"
)

func registerAgentPackageRoutes(r chi.Router) {
	r.Get("/plugins/*", serveAgentPackage())
}

func serveAgentPackage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
		ok, err := agentpackages.IsServedFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := agentpackages.Render(path, agentpackages.Options{
			RegistryBaseURL: requestBaseURL(r),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		switch {
		case strings.HasSuffix(path, ".sh"):
			w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		case strings.HasSuffix(path, ".json"):
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		case strings.HasSuffix(path, ".toml"):
			w.Header().Set("Content-Type", "application/toml; charset=utf-8")
		case strings.HasSuffix(path, ".md"), strings.HasSuffix(path, ".mdc"):
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		case strings.HasSuffix(path, ".svg"):
			w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, _ = w.Write([]byte(body))
	}
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Clamp the forwarded proto to known schemes — it is request-controlled and
	// flows into served install scripts/configs.
	if proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); proto == "http" || proto == "https" {
		scheme = proto
	}
	host := sanitizeHTTPHost(r.Host)
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// sanitizeHTTPHost returns the request Host only if it is a well-formed
// host[:port] (letters, digits, '.', '-', ':', and IPv6 brackets). The Host
// header is attacker-controlled and is interpolated into served shell
// scripts/config templates, so anything else — quotes, spaces, shell
// metacharacters, newlines — yields "" rather than risk script injection.
func sanitizeHTTPHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || len(host) > 253 {
		return ""
	}
	for _, c := range host {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '[', c == ']':
		default:
			return ""
		}
	}
	return host
}
