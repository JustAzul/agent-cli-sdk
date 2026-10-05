package e2e

import (
	"path/filepath"
	"reflect"
	"testing"
)

func dryArgv(t *testing.T, r result) []any {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d stderr %s", r.code, r.stderr)
	}
	return r.json(t)["argv"].([]any)
}

func TestProfileApplied(t *testing.T) {
	s := newSandbox(t)
	repo := gitRepo(t)
	argv := dryArgv(t, s.run("exec", "--dry-run", "--run-id", "pr1", "--cwd", repo, "--scenario", "delegation", "q"))
	out := filepath.Join(s.home, "runs", "pr1", "output.md")
	want := []any{"codex", "exec", "-C", repo, "-s", "workspace-write", "-m", "gpt-6.1-sol",
		"-c", "model_reasoning_effort=medium", "--json", "-o", out, "-"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv\n got %v\nwant %v", argv, want)
	}
}

func TestProfileFlagOverridesAndSources(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	argv := dryArgv(t, s.run("exec", "--dry-run", "--cwd", gitRepo(t), "--scenario", "delegation", "--effort", "high", "q"))
	if !containsStr(argv, "model_reasoning_effort=high") || containsStr(argv, "model_reasoning_effort=medium") {
		t.Errorf("effort flag did not win: %v", argv)
	}

	r := s.run("exec", "--run-id", "ps1", "--scenario", "delegation", "--effort", "high", "q")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	req := readJSONFile(t, filepath.Join(s.home, "runs", "ps1", "request.json"))
	for k, want := range map[string]any{
		"model": "gpt-6.1-sol", "model_source": "profile",
		"effort": "high", "effort_source": "flag",
		"sandbox": "workspace-write", "sandbox_source": "profile",
	} {
		if req[k] != want {
			t.Errorf("request.%s = %v, want %v", k, req[k], want)
		}
	}
}

func TestUnknownScenarioHasNoProfile(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	repo := gitRepo(t)
	argv := dryArgv(t, s.run("exec", "--dry-run", "--cwd", repo, "--scenario", "claude-md-update", "q"))
	for _, a := range []string{"-s", "-m", "-c"} {
		if containsStr(argv, a) {
			t.Errorf("unexpected %s in %v", a, argv)
		}
	}
	r := s.run("exec", "--run-id", "us1", "--cwd", repo, "--scenario", "claude-md-update", "q")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	req := readJSONFile(t, filepath.Join(s.home, "runs", "us1", "request.json"))
	if req["scenario"] != "claude-md-update" || req["model"] != nil || req["model_source"] != "provider-default" ||
		req["effort_source"] != "provider-default" || req["sandbox_source"] != "provider-default" {
		t.Errorf("request.json = %v", req)
	}
}
