// Package observability wires optional Sentry reporting (see docs/spec.md §13).
package observability

import (
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/specgate/doc-registry/internal/config"
)

// InitSentry configures the global Sentry client when SENTRY_DSN is set.
// cleanup must be called on process shutdown (typically defer after main init).
func InitSentry(cfg *config.Config) (cleanup func(), err error) {
	cleanup = func() {}
	if cfg.Sentry.DSN == "" {
		return cleanup, nil
	}

	err = sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.Sentry.DSN,
		Environment:      cfg.Sentry.Environment,
		Release:          cfg.Sentry.Release,
		TracesSampleRate: cfg.Sentry.TracesSampleRate,
	})
	if err != nil {
		return nil, err
	}

	cleanup = func() {
		sentry.Flush(2 * time.Second)
	}
	return cleanup, nil
}

// SentryHTTPMiddleware returns chi middleware that reports panics to Sentry.
// Use only after sentry.Init; when DSN is empty this is not registered.
func SentryHTTPMiddleware() func(http.Handler) http.Handler {
	return sentryhttp.New(sentryhttp.Options{
		Repanic:         false,
		WaitForDelivery: false,
	}).Handle
}

// SentryRequestIDTags copies chi request ID onto the Sentry scope when a hub exists.
// Place immediately after SentryHTTPMiddleware in the middleware chain.
func SentryRequestIDTags() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
				if rid := middleware.GetReqID(r.Context()); rid != "" {
					hub.Scope().SetTag("request_id", rid)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
