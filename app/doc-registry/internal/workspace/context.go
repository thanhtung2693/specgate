package workspace

import (
	"context"
	"strings"
	"unicode"
)

type contextKey struct{}

// WithID attaches the trusted workspace selected by the transport/session.
// Domain packages read this value as a fallback so one request cannot lose its
// scope while crossing service boundaries.
func WithID(ctx context.Context, id string) context.Context {
	id, _ = NormalizeID(id)
	return context.WithValue(ctx, contextKey{}, id)
}

// ID returns the trusted workspace attached to the current operation.
func ID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(contextKey{}).(string)
	return strings.TrimSpace(id)
}

// NormalizeID accepts one opaque workspace path segment. Workspace IDs become
// object-store and local-filesystem prefixes, so separators, traversal-like
// values, and control characters are never valid IDs.
func NormalizeID(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if id == "" || id == "." || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) ||
		strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return "", false
	}
	return id, true
}
