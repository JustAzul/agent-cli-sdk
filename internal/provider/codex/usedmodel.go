package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// UsedModel reads the model and effort codex ran a session with from its own
// session file: the last turn_context of
// $CODEX_HOME/sessions/<y>/<m>/<d>/rollout-<time>-<session id>.jsonl, which is
// the current turn's on a resumed session. It depends on that file layout: no
// such file yields "" for both, and one that cannot be read an error.
func (adapter) UsedModel(env []string, sessionID string) (model, effort string, err error) {
	if sessionID == "" || strings.ContainsAny(sessionID, `*?[]\/`) {
		return "", "", nil
	}
	home := codexHome(env)
	if home == "" {
		return "", "", nil
	}
	matches, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", "", err
	}
	return lastTurnContext(matches[len(matches)-1])
}

// codexHome is where codex keeps its state for the run's environment.
func codexHome(env []string) string {
	if home := lookupEnv(env, "CODEX_HOME"); home != "" {
		return home
	}
	if home := lookupEnv(env, "HOME"); home != "" {
		return filepath.Join(home, ".codex")
	}
	return ""
}

func lookupEnv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v
		}
	}
	return ""
}

// lastTurnContext reads the file line by line, each line whole however long
// (a large tool output), and keeps the last turn_context's model and effort.
func lastTurnContext(path string) (model, effort string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	marker := []byte(`"turn_context"`)
	r := bufio.NewReader(f)
	for {
		line, readErr := r.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", "", fmt.Errorf("reading %s: %w", path, readErr)
		}
		if bytes.Contains(line, marker) {
			if m, e, ok := turnContext(line); ok {
				model, effort = m, e
			}
		}
		if readErr != nil {
			return model, effort, nil
		}
	}
}

func turnContext(line []byte) (model, effort string, ok bool) {
	var item struct {
		Type    string `json:"type"`
		Payload struct {
			Model  string `json:"model"`
			Effort string `json:"effort"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &item) != nil || item.Type != "turn_context" || item.Payload.Model == "" {
		return "", "", false
	}
	return item.Payload.Model, item.Payload.Effort, true
}
