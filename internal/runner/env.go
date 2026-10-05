package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// buildEnv starts from the caller's environment (FR12), then applies the
// plan's additions and removals.
func buildEnv(base []string, add map[string]string, remove []string) []string {
	drop := map[string]bool{}
	for _, k := range remove {
		drop[k] = true
	}
	for k := range add {
		drop[k] = true
	}
	out := make([]string, 0, len(base)+len(add))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	for k, v := range add {
		out = append(out, k+"="+v)
	}
	return out
}

// lookPath resolves a binary against the caller environment's PATH.
func lookPath(file string, env []string) (string, error) {
	if strings.ContainsRune(file, '/') {
		return file, checkExecutable(file)
	}
	pathVar := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			pathVar = v
		}
	}
	for _, dir := range filepath.SplitList(pathVar) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, file)
		if checkExecutable(candidate) == nil {
			return candidate, nil
		}
	}
	return "", errors.New("not found on PATH")
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return os.ErrPermission
	}
	return nil
}
