package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exec-ok's provider session, as its thread.started reports it.
const execOKSession = "00000000-0000-4000-8000-000000000001"

func codexHomeWithModel(t *testing.T, model, effort string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "10", "07")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"turn_context","payload":{"model":"` + model + `","effort":"` + effort + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-10-07T00-00-00-"+execOKSession+".jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func runsByID(t *testing.T, r result) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, line := range r.lines() {
		var run map[string]any
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			t.Fatalf("runs line %q: %v", line, err)
		}
		out[run["run_id"].(string)] = run
	}
	return out
}

func TestTelemetryRecordsTheModelTheProviderUsed(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	s.set("CODEX_HOME", codexHomeWithModel(t, "gpt-used", "high"))
	s.run("exec", "--run-id", "mu1", "q")
	s.set("CODEX_HOME", t.TempDir())
	s.run("exec", "--run-id", "mu2", "--model", "gpt-asked", "q")

	runs := runsByID(t, s.run("runs"))

	if runs["mu1"]["model_used"] != "gpt-used" || runs["mu1"]["effort_used"] != "high" || runs["mu1"]["model"] != nil {
		t.Errorf("mu1: model=%v model_used=%v effort_used=%v", runs["mu1"]["model"], runs["mu1"]["model_used"], runs["mu1"]["effort_used"])
	}
	if runs["mu2"]["model_used"] != nil || runs["mu2"]["model"] != "gpt-asked" {
		t.Errorf("mu2: model=%v model_used=%v", runs["mu2"]["model"], runs["mu2"]["model_used"])
	}
}

func TestStatsAndRunsNarrowToOneSession(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	s.set("CODEX_HOME", codexHomeWithModel(t, "gpt-used", "high"))
	s.run("exec", "--run-id", "ss1", "--session-id", "sess-a", "q")
	s.run("exec", "--run-id", "ss2", "--session-id", "sess-a", "--model", "gpt-asked", "q")
	s.run("exec", "--run-id", "ss3", "--session-id", "sess-b", "q")

	runs := runsByID(t, s.run("runs", "--session-id", "sess-a"))
	stats := s.run("stats", "--session-id", "sess-a", "--json").json(t)

	if len(runs) != 2 || runs["ss1"] == nil || runs["ss2"] == nil {
		t.Errorf("runs --session-id sess-a = %v", runs)
	}
	if stats["total"] != float64(2) {
		t.Errorf("stats total = %v, want 2", stats["total"])
	}
	// exec-ok reports 18573 input and 5 output tokens per run; both runs ran on
	// gpt-used, the model the provider reported, whatever the second asked for.
	byModel := stats["usage_by_model"].(map[string]any)
	used, _ := byModel["gpt-used"].(map[string]any)
	if len(byModel) != 1 || used["input_tokens"] != float64(37146) || used["output_tokens"] != float64(10) || used["runs_with_usage"] != float64(2) {
		t.Errorf("usage_by_model = %v", byModel)
	}
	plain := s.run("stats", "--session-id", "sess-a")
	if !strings.Contains(plain.stdout, "tokens gpt-used: input_tokens=37146") {
		t.Errorf("stats summary = %q", plain.stdout)
	}
}
