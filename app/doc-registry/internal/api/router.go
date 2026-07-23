package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/observability"
)

// Router wires REST endpoints per spec §6 and registers them on a Huma API
// with generated OpenAPI 3.1 and Scalar reference pages.
//
// Local dev: no HTTP authentication — all routes are open (see docs).
type Router struct {
	Handlers *Handlers
	Config   *config.Config
	// SentryMiddleware optional — when set, replaces chi Recoverer for panic reporting (spec §13).
	SentryMiddleware func(http.Handler) http.Handler
	// Logger is used for per-request access logging. Nil disables HTTP logging
	// (kept optional so tests can build a Router without threading a logger).
	Logger *zerolog.Logger
}

func (rt *Router) Build() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if rt.SentryMiddleware != nil {
		r.Use(rt.SentryMiddleware)
		r.Use(observability.SentryRequestIDTags())
	} else {
		r.Use(middleware.Recoverer)
	}
	if rt.Logger != nil {
		r.Use(RequestLogger(*rt.Logger))
	}
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cliWorkspaceMiddleware)

	// Resolve the OAuth callback base URL per request (env override else derived
	// from the request) so both the authorize and callback handlers agree.
	var oauthCallbackOverride string
	if rt.Handlers != nil {
		oauthCallbackOverride = rt.Handlers.OAuthCallbackBaseURL
	}
	r.Use(oauthCallbackBaseMiddleware(oauthCallbackOverride))

	humaCfg := huma.DefaultConfig("Doc Registry", "1.0.0")
	if rt.Config.OpenAPI.Enabled {
		humaCfg.DocsPath = "/docs"
		humaCfg.DocsRenderer = huma.DocsRendererScalar
		humaCfg.OpenAPIPath = "/openapi"
	} else {
		// Empty paths disable the reference UI and OpenAPI document. Routes still register
		// and serve traffic normally.
		humaCfg.DocsPath = ""
		humaCfg.OpenAPIPath = ""
	}

	api := humachi.New(r, humaCfg)
	rt.registerRoutes(api)
	rt.registerOAuthRoutes(r)
	registerAgentPackageRoutes(r)
	r.Get("/cli/install.sh", serveCLIInstallScript)
	if rt.Handlers != nil {
		// Governance file bodies are larger binary uploads, so they retain raw
		// content routes outside the JSON API.
		r.Get("/governance/files/{id}/content", rt.Handlers.ServeGovernanceFileContent)
		r.Post("/governance/files/upload", rt.Handlers.UploadGovernanceFile)
	}

	return r
}

func (rt *Router) registerOAuthRoutes(r chi.Router) {
	h := rt.Handlers

	// GET /integrations/oauth-callback is a redirect handler (302 response) that
	// cannot be expressed as a huma JSON operation, so it lives here on the plain
	// chi router. The authorize and disconnect endpoints are registered via huma
	// in registerRoutes for OpenAPI visibility.
	r.Get("/integrations/oauth-callback", func(w http.ResponseWriter, req *http.Request) {
		if h == nil || h.Integrations == nil {
			http.Error(w, "integrations service not configured", http.StatusServiceUnavailable)
			return
		}
		// The callback is served by the backend, which in dev is a different
		// origin than the SPA, so redirect to the UI's public origin
		// (APP_BASE_URL) joined with the validated app-relative target.
		// Empty AppBaseURL keeps it relative (same-origin / reverse-proxy prod).
		redirectToApp := func(rel string) {
			loc := rel
			if base := strings.TrimRight(strings.TrimSpace(h.AppBaseURL), "/"); base != "" {
				loc = base + rel
			}
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusFound)
		}
		result, err := h.Integrations.CompleteOAuthCallback(req.Context(), req.URL.Query().Get("state"), req.URL.Query().Get("code"), oauthCallbackBaseFromContext(req.Context()))
		if err != nil {
			// Log the real error server-side so it is visible in container logs
			// even though we never leak the detail into the redirect URL.
			if rt.Logger != nil {
				rt.Logger.Error().Err(err).Str("path", req.URL.Path).Msg("oauth callback failed")
			}
			redirectToApp(integrations.OAuthErrorRedirect())
			return
		}
		redirectToApp(integrations.OAuthResultRedirect(result.RedirectTarget, "oauth", "connected"))
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
