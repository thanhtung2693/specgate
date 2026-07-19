// Package buildinfo exposes the process version injected at link time.
package buildinfo

// Version is the semantic version or git tag injected at build time.
// Inject with: -ldflags "-X github.com/specgate/doc-registry/internal/buildinfo.Version=v1.2.3"
var Version = "dev"
