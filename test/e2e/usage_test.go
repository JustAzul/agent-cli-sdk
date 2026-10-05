package e2e

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUnknownCommandAndFlagsAreUsageErrors(t *testing.T) {
	s := newSandbox(t)
	if r := s.run("nope"); r.code != 2 {
		t.Errorf("unknown command: exit %d", r.code)
	}
	if r := s.run(); r.code != 2 {
		t.Errorf("no command: exit %d", r.code)
	}
	if r := s.run("exec", "--no-such-flag", "q"); r.code != 2 || r.stderr == "" {
		t.Errorf("unknown flag: exit %d stderr %q", r.code, r.stderr)
	}
}

func TestDryRunPlan(t *testing.T) {
	s := newSandbox(t)
	repo := gitRepo(t)
	r := s.run("exec", "--dry-run", "--run-id", "dry1", "--cwd", repo, "--model", "m",
		"--effort", "high", "--sandbox", "read-only", "q")
	if r.code != 0 {
		t.Fatalf("exit %d stderr %s", r.code, r.stderr)
	}
	plan := r.json(t)
	out := filepath.Join(s.home, "runs", "dry1", "output.md")
	want := []any{"codex", "exec", "-C", repo, "-s", "read-only", "-m", "m",
		"-c", "model_reasoning_effort=high", "--json", "-o", out, "-"}
	if !reflect.DeepEqual(plan["argv"], want) {
		t.Errorf("argv\n got %v\nwant %v", plan["argv"], want)
	}
	if plan["stdin"] != "prompt" || plan["cwd"] != repo {
		t.Errorf("stdin=%v cwd=%v", plan["stdin"], plan["cwd"])
	}
	if env, ok := plan["env_add"].(map[string]any); !ok || len(env) != 0 {
		t.Errorf("env_add = %v", plan["env_add"])
	}
	if exists(s.home) {
		t.Errorf("dry-run created %s", s.home)
	}
}

func TestDryRunNeverEphemeralAndGitFlag(t *testing.T) {
	s := newSandbox(t)
	outside := t.TempDir()
	r := s.run("exec", "--dry-run", "--cwd", outside, "q")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	argv := r.json(t)["argv"].([]any)
	if !containsStr(argv, "--skip-git-repo-check") {
		t.Errorf("outside git: no --skip-git-repo-check in %v", argv)
	}
	if containsStr(argv, "--ephemeral") {
		t.Errorf("--ephemeral in %v", argv)
	}

	r = s.run("exec", "--dry-run", "--cwd", gitRepo(t), "q")
	argv = r.json(t)["argv"].([]any)
	if containsStr(argv, "--skip-git-repo-check") || containsStr(argv, "--ephemeral") {
		t.Errorf("inside git: unexpected flags in %v", argv)
	}
}

func TestDryRunPassthroughAfterSDKFlags(t *testing.T) {
	s := newSandbox(t)
	r := s.run("exec", "--dry-run", "--cwd", gitRepo(t), "q", "--", "-c", "features.web_search=true", "--add-dir", "/tmp/x")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	argv := r.json(t)["argv"].([]any)
	n := len(argv)
	tail := []any{"-c", "features.web_search=true", "--add-dir", "/tmp/x", "-"}
	if !reflect.DeepEqual(argv[n-5:], tail) {
		t.Errorf("tail = %v", argv[n-5:])
	}
}

func TestPromptSourceValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"two sources", []string{"exec", "--prompt-file", "p.md", "q"}},
		{"no prompt", []string{"exec"}},
		{"two positionals", []string{"exec", "a", "b"}},
		{"missing prompt file", []string{"exec", "--prompt-file", "/no/such/file"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t)
			rec := s.recordTo()
			r := s.run(c.args...)
			if r.code != 2 {
				t.Errorf("exit %d, want 2 (stderr %s)", r.code, r.stderr)
			}
			if exists(rec) {
				t.Error("provider was invoked")
			}
			if exists(filepath.Join(s.home, "runs")) {
				t.Error("run directory created")
			}
		})
	}
}

func TestSandboxValidation(t *testing.T) {
	s := newSandbox(t)
	for _, v := range []string{"danger-full-access", "bogus"} {
		if r := s.run("exec", "--dry-run", "--sandbox", v, "q"); r.code != 2 {
			t.Errorf("--sandbox %s: exit %d", v, r.code)
		}
	}
	for _, v := range []string{"read-only", "workspace-write"} {
		if r := s.run("exec", "--dry-run", "--sandbox", v, "q"); r.code != 0 {
			t.Errorf("--sandbox %s: exit %d", v, r.code)
		}
	}
}

func TestUnknownProviderIsUsageError(t *testing.T) {
	s := newSandbox(t)
	if r := s.run("exec", "--provider", "nope", "q"); r.code != 2 {
		t.Errorf("exit %d", r.code)
	}
}

func TestRunIDValidation(t *testing.T) {
	for _, bad := range []string{"a b", "..", ".hidden", "-x"} {
		s := newSandbox(t)
		r := s.run("exec", "--run-id="+bad, "q")
		if r.code != 2 {
			t.Errorf("--run-id %q: exit %d", bad, r.code)
		}
		if exists(filepath.Join(s.home, "runs")) {
			t.Errorf("--run-id %q created a runs directory", bad)
		}
	}
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	r := s.run("exec", "--run-id", "ok.id-1", "q")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	if !exists(filepath.Join(s.home, "runs", "ok.id-1", "state.json")) {
		t.Error("run directory not named after the run id")
	}
	if r := s.run("exec", "--run-id", "ok.id-1", "q"); r.code != 2 {
		t.Errorf("reused run id: exit %d", r.code)
	}
	if r := s.run("exec", "--dry-run", "--run-id", "ok.id-1", "q"); r.code != 2 {
		t.Errorf("dry-run with existing run id: exit %d", r.code)
	}
}

func TestGeneratedRunIDShape(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	r := s.run("exec", "--json", "q")
	m := r.json(t)
	id, _ := m["run_id"].(string)
	cid, _ := m["conversation_id"].(string)
	if !strings.HasPrefix(id, "r-") || !strings.HasPrefix(cid, "c-") || len(id) != len("r-20261004T231500Z-a1b2c3d4") {
		t.Errorf("ids: %q %q", id, cid)
	}
	if _, err := os.Stat(filepath.Join(s.home, "runs", id)); err != nil {
		t.Error(err)
	}
}

func containsStr(list []any, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
