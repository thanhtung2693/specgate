package mcp

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// parseBearerToken extracts the token from an "Authorization: Bearer <token>" header.
// The "Bearer" scheme is matched case-insensitively per HTTP spec.
// Returns empty string if the header is absent or malformed.
func parseBearerToken(auth string) string {
	const prefix = "Bearer "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return auth[len(prefix):]
}

// AuthMiddleware wraps an http.Handler with Bearer token authentication.
func AuthMiddleware(apiKey string, next http.Handler) http.Handler {
	expected := []byte(apiKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := []byte(parseBearerToken(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(token, expected) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
