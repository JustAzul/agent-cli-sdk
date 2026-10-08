package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// catalogTimeout bounds the `codex debug models` call.
const catalogTimeout = 10 * time.Second

// PriceNamespace is the litellm_provider of the price-list entries of the
// models codex offers.
func (adapter) PriceNamespace() string { return "openai" }

// CatalogModels lists the model slugs `codex debug models` prints. The binary
// is resolved on the PATH of env and runs with env, without standard input,
// and is stopped after ten seconds.
func (adapter) CatalogModels(env []string) ([]string, error) {
	bin, err := lookPath("codex", env)
	if err != nil {
		return nil, fmt.Errorf("codex debug models: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "debug", "models")
	cmd.Env = env
	cmd.WaitDelay = time.Second // a grandchild holding the pipe must not outlast the timeout
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if tail := strings.TrimSpace(string(exit.Stderr)); tail != "" {
				return nil, fmt.Errorf("codex debug models: %w: %s", err, tail)
			}
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("codex debug models: timed out after %s", catalogTimeout)
		}
		return nil, fmt.Errorf("codex debug models: %w", err)
	}
	return parseCatalog(out)
}

// parseCatalog reads {"models":[{"slug":...}]}: the distinct non-empty slugs
// in the order given.
func parseCatalog(out []byte) ([]string, error) {
	var doc struct {
		Models *[]struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("codex debug models: output is not JSON: %w", err)
	}
	if doc.Models == nil {
		return nil, errors.New("codex debug models: output has no models list")
	}
	slugs := []string{}
	for _, m := range *doc.Models {
		if m.Slug != "" && !slices.Contains(slugs, m.Slug) {
			slugs = append(slugs, m.Slug)
		}
	}
	return slugs, nil
}

// lookPath resolves a binary against the PATH of env, not the process's.
func lookPath(file string, env []string) (string, error) {
	pathVar := lookupEnv(env, "PATH")
	for _, dir := range filepath.SplitList(pathVar) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, file)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", errors.New("codex not found on PATH")
}
