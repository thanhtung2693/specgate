package buildinfo

// Injected at build time via ldflags: -X github.com/specgate/specgate/app/cli/internal/buildinfo.Version=...
var Version = "dev"
var Commit = "unknown"
var Date = "unknown"
