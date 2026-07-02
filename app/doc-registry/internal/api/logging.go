package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// RequestLogger emits one structured log line per HTTP request with method,
// path, status, duration, and the chi request ID. Doc Registry has no HTTP
// auth (spec §7), so the request ID is the primary correlation key between
// the API log and downstream job/observability tooling.
func RequestLogger(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			evt := log.Info()
			if ww.Status() >= 500 {
				evt = log.Error()
			} else if ww.Status() >= 400 {
				evt = log.Warn()
			}
			evt.
				Str("request_id", middleware.GetReqID(r.Context())).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Int("bytes", ww.BytesWritten()).
				Dur("duration", time.Since(start)).
				Msg("http")
		})
	}
}
