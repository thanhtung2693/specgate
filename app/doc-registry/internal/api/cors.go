package api

import (
	"net/http"
	"net/url"
	"strings"
)

// DevCORS wraps the API so CORS runs before any Chi/Huma middleware (preflight must get Allow-* headers).
func DevCORS(next http.Handler) http.Handler {
	return devCorsMiddleware(next)
}

// devCorsMiddleware allows the Vite dev server (and preview) to call the API from another origin.
func devCorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		allowed := allowedDevOrigin(origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			// Preflight sends Access-Control-Request-Headers (often "content-type"); echo it so the
			// browser accepts the response (fixed header list alone can fail strict checks).
			if acrh := r.Header.Get("Access-Control-Request-Headers"); acrh != "" {
				w.Header().Set("Access-Control-Allow-Headers", acrh)
			} else {
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Accept")
			}
		}
		// Never answer preflight with 204 unless CORS headers were set — otherwise the browser
		// reports "No Access-Control-Allow-Origin" (e.g. unknown Origin like http://[::1]:3000).
		if r.Method == http.MethodOptions && allowed {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedDevOrigin(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
