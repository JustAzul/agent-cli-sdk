package codex_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/provider/codex"
)

func gitDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBuildPlanExecArgvOrder(t *testing.T) {
	cwd := gitDir(t)
	p := codex.New()
	plan, err := p.BuildPlan(provider.Request{
		Command: "exec", Cwd: cwd, Model: "m", Effort: "high", Sandbox: "read-only",
		OutputPath: "/out/output.md", Passthrough: []string{"--add-dir", "/tmp/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "exec", "-C", cwd, "-s", "read-only", "-m", "m",
		"-c", "model_reasoning_effort=high", "--json", "-o", "/out/output.md",
		"--add-dir", "/tmp/x", "-"}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("argv\n got %q\nwant %q", plan.Argv, want)
	}
	if plan.Stdin != provider.StdinPrompt || plan.Dir != cwd {
		t.Fatalf("stdin=%q dir=%q", plan.Stdin, plan.Dir)
	}
}

func TestBuildPlanOmitsUnsetFlags(t *testing.T) {
	cwd := gitDir(t)
	plan, err := codex.New().BuildPlan(provider.Request{Command: "exec", Cwd: cwd, OutputPath: "/o"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "exec", "-C", cwd, "--json", "-o", "/o", "-"}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("argv\n got %q\nwant %q", plan.Argv, want)
	}
}

func TestBuildPlanSkipGitRepoCheckOutsideGit(t *testing.T) {
	outside := t.TempDir()
	plan, _ := codex.New().BuildPlan(provider.Request{Command: "exec", Cwd: outside, OutputPath: "/o", Passthrough: []string{"--x"}})
	got := strings.Join(plan.Argv, " ")
	if !strings.Contains(got, "--skip-git-repo-check") {
		t.Fatalf("missing flag: %s", got)
	}
	if !strings.Contains(got, "-o /o --skip-git-repo-check --x -") {
		t.Fatalf("flag must follow SDK flags and precede passthrough: %s", got)
	}
	// nested directory inside a repo is still inside a work tree
	repo := gitDir(t)
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, _ = codex.New().BuildPlan(provider.Request{Command: "exec", Cwd: nested, OutputPath: "/o"})
	if strings.Contains(strings.Join(plan.Argv, " "), "--skip-git-repo-check") {
		t.Fatalf("flag added inside git work tree: %q", plan.Argv)
	}
}

func TestBuildPlanNeverEphemeral(t *testing.T) {
	plan, _ := codex.New().BuildPlan(provider.Request{Command: "exec", Cwd: t.TempDir(), OutputPath: "/o"})
	for _, a := range plan.Argv {
		if a == "--ephemeral" {
			t.Fatal("--ephemeral present")
		}
	}
}

func TestCapabilities(t *testing.T) {
	c := codex.New().Capabilities()
	if !c.Supports("exec") || !c.SupportsSandbox("read-only") || !c.SupportsSandbox("workspace-write") || c.SupportsSandbox("danger-full-access") {
		t.Fatalf("capabilities: %+v", c)
	}
}

func TestParseEvent(t *testing.T) {
	p := codex.New()
	cases := []struct {
		name, line string
		ok         bool
		want       provider.Event
	}{
		{"thread started", `{"type":"thread.started","thread_id":"T1"}`, true, provider.Event{SessionID: "T1"}},
		{"usage", `{"type":"turn.completed","usage":{"input_tokens":3,"cached_input_tokens":2,"cache_write_input_tokens":1,"output_tokens":5,"reasoning_output_tokens":4}}`, true,
			provider.Event{Usage: &provider.Usage{InputTokens: 3, CachedInputTokens: 2, CacheWriteInputTokens: 1, OutputTokens: 5, ReasoningOutputTokens: 4}}},
		{"error plain", `{"type":"error","message":"boom"}`, true, provider.Event{ErrorMsg: "boom"}},
		{"error wrapped", `{"type":"error","message":"{\"type\":\"error\",\"error\":{\"message\":\"inner\"}}"}`, true, provider.Event{ErrorMsg: "inner"}},
		{"turn failed wrapped", `{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"inner2\"}}"}}`, true, provider.Event{ErrorMsg: "inner2"}},
		{"turn failed plain", `{"type":"turn.failed","error":{"message":"plain"}}`, true, provider.Event{ErrorMsg: "plain"}},
		{"json message without inner", `{"type":"error","message":"{\"a\":1}"}`, true, provider.Event{ErrorMsg: `{"a":1}`}},
		{"item error ignored", `{"type":"item.completed","item":{"type":"error","message":"warn"}}`, true, provider.Event{}},
		{"unknown type", `{"type":"brand.new"}`, true, provider.Event{}},
		{"garbage", `not json`, false, provider.Event{}},
		{"non object", `42`, false, provider.Event{}},
	}
	for _, c := range cases {
		got, ok := p.ParseEvent([]byte(c.line))
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got (%+v, %v) want (%+v, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
