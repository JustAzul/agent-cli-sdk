// Package version holds build metadata injected through ldflags.
package version

import "strconv"

// Set at build time with -ldflags "-X .../internal/version.Version=...".
var (
	Version      = "dev"
	SourceCommit = "unknown"
	BuildSeq     = "0"
)

// BuildSeqNumber returns BuildSeq as an integer; a malformed value is 0.
func BuildSeqNumber() int {
	n, err := strconv.Atoi(BuildSeq)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
