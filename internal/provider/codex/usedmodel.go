package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// rolloutLineBytes bounds one line of a session file; a longer line (a large
// tool output) is skipped, never a turn_context.
const rolloutLineBytes = 16 * 1024 * 1024

// UsedModel reads the model and effort codex ran a session with from its own
// session file: the last turn_context of
// $CODEX_HOME/sessions/<y>/<m>/<d>/rollout-<time>-<session id>.jsonl, which is
// the current turn's on a resumed session. It depends on that file layout, so
// a missing or unreadable file yields "" for both.
func (adapter) UsedModel(env []string, sessionID string) (model, effort string) {
	if sessionID == "" || strings.ContainsAny(sessionID, `*?[]\/`) {
		return "", ""
	}
	home := codexHome(env)
	if home == "" {
		return "", ""
	}
	matches, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", ""
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

func lastTurnContext(path string) (model, effort string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	marker := []byte(`"turn_context"`)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), rolloutLineBytes)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, marker) {
			continue
		}
		var item struct {
			Type    string `json:"type"`
			Payload struct {
				Model  string `json:"model"`
				Effort string `json:"effort"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &item) == nil && item.Type == "turn_context" && item.Payload.Model != "" {
			model, effort = item.Payload.Model, item.Payload.Effort
		}
	}
	return model, effort
}
