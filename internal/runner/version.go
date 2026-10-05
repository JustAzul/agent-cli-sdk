package runner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

const versionTimeout = 5 * time.Second

// providerVersion runs the provider's version command and returns its first
// output line, or nil when it fails, prints nothing or takes too long.
func providerVersion(path string, args []string, dir string, env []string) *string {
	if len(args) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if v := strings.TrimSpace(line); v != "" {
			return &v
		}
	}
	return nil
}
