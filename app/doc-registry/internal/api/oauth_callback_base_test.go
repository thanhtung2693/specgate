package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDerivePublicBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		tls     bool
		headers map[string]string
		want    string
	}{
		{name: "direct host", host: "127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "tls terminated here", host: "registry.example", tls: true, want: "https://registry.example"},
		{
			name:    "forwarded by proxy",
			host:    "127.0.0.1:8080",
			headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "app.specgate.io"},
			want:    "https://app.specgate.io",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Host = tc.host
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if got := derivePublicBaseURL(req); got != tc.want {
				t.Fatalf("derivePublicBaseURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOAuthCallbackBaseMiddleware_OverrideElseDerived(t *testing.T) {
	capture := func(override, host string) string {
		var seen string
		h := oauthCallbackBaseMiddleware(override)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen = oauthCallbackBaseFromContext(r.Context())
		}))
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Host = host
		h.ServeHTTP(httptest.NewRecorder(), req)
		return seen
	}
	if got := capture("https://app.specgate.io", "127.0.0.1:8080"); got != "https://app.specgate.io" {
		t.Fatalf("override must win, got %q", got)
	}
	if got := capture("", "127.0.0.1:8080"); got != "http://127.0.0.1:8080" {
		t.Fatalf("empty override must derive from request, got %q", got)
	}
}
