package codex_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/provider/codex"
)

const sessionID = "00000000-0000-4000-8000-000000000004"

// writeRollout lays out a session file the way codex names it under home.
func writeRollout(t *testing.T, home string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "10", "07")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "rollout-2026-10-07T00-28-12-" + sessionID + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func usedModel(env []string, id string) (string, string) {
	return codex.New().(provider.ModelReporter).UsedModel(env, id)
}

func TestUsedModelIsTheLastTurnContext(t *testing.T) {
	home := t.TempDir()
	writeRollout(t, home,
		`{"type":"session_meta","payload":{"model_provider":"openai"}}`,
		`{"type":"turn_context","payload":{"model":"gpt-first","effort":"low"}}`,
		`{"type":"response_item","payload":{"text":"`+strings.Repeat("x", 200000)+`"}}`,
		`{"type":"turn_context","payload":{"model":"gpt-second","effort":"high"}}`,
	)

	model, effort := usedModel([]string{"CODEX_HOME=" + home}, sessionID)

	if model != "gpt-second" || effort != "high" {
		t.Errorf("got %q %q, want gpt-second high", model, effort)
	}
}

func TestUsedModelFindsCodexUnderHome(t *testing.T) {
	home := t.TempDir()
	writeRollout(t, filepath.Join(home, ".codex"), `{"type":"turn_context","payload":{"model":"gpt-home","effort":"medium"}}`)

	model, effort := usedModel([]string{"HOME=" + home}, sessionID)

	if model != "gpt-home" || effort != "medium" {
		t.Errorf("got %q %q, want gpt-home medium", model, effort)
	}
}

func TestUsedModelCannotTell(t *testing.T) {
	home := t.TempDir()
	writeRollout(t, home, `{"type":"session_meta","payload":{}}`)
	env := []string{"CODEX_HOME=" + home}
	cases := []struct {
		name string
		env  []string
		id   string
	}{
		{"no turn context", env, sessionID},
		{"no session file", env, "00000000-0000-4000-8000-000000000005"},
		{"glob in the id", env, "*"},
		{"empty id", env, ""},
		{"no home", nil, sessionID},
	}
	for _, c := range cases {
		if model, effort := usedModel(c.env, c.id); model != "" || effort != "" {
			t.Errorf("%s: got %q %q, want nothing", c.name, model, effort)
		}
	}
}
