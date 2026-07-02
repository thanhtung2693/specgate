// Package buildinfo exposes process-level build metadata injected at link time
// via -ldflags. Defaults to "dev" / "unknown" when not set (local builds,
// tests).
package buildinfo

// Version is the semantic version or git tag injected at build time.
// Inject with: -ldflags "-X github.com/specgate/doc-registry/internal/buildinfo.Version=v1.2.3"
var Version = "dev"

// Commit is the short git SHA injected at build time.
var Commit = "unknown"

// Date is the ISO-8601 build timestamp injected at build time.
var Date = "unknown"
