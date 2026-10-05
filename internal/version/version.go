// Package version holds build metadata injected through ldflags.
package version

// Set at build time with -ldflags "-X .../internal/version.Version=...".
var (
	Version      = "dev"
	SourceCommit = "unknown"
	BuildSeq     = "0"
)
