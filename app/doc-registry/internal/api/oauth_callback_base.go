package api

import (
	"context"
	"net/http"
	"strings"
)

type oauthBaseCtxKey struct{}

// oauthCallbackBaseMiddleware resolves the public base URL used to build the
// OAuth redirect_uri and stashes it on the request context, so the begin
// (authorize) and the callback handlers derive the same value — the provider
// rejects a mismatch between the authorize request and the token exchange.
//
// An explicit override (OAUTH_PUBLIC_CALLBACK_BASE_URL) wins; it is only needed
// for reverse-proxy / prod setups where the request host is not the public
// origin. Otherwise the base is derived from the request, so local dev needs no
// configuration.
func oauthCallbackBaseMiddleware(override string) func(http.Handler) http.Handler {
	override = strings.TrimSpace(override)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			base := override
			if base == "" {
				base = derivePublicBaseURL(req)
			}
			ctx := context.WithValue(req.Context(), oauthBaseCtxKey{}, base)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// derivePublicBaseURL builds scheme://host from an inbound request: the
// X-Forwarded-Proto / X-Forwarded-Host headers when a proxy sets them, else the
// request's own scheme (https when TLS-terminated here) and Host header.
func derivePublicBaseURL(req *http.Request) string {
	scheme := "http"
	if p := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")); p != "" {
		scheme = p
	} else if req.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(req.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = req.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func oauthCallbackBaseFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(oauthBaseCtxKey{}).(string); ok {
		return v
	}
	return ""
}
