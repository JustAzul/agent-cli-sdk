package e2e

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAttrsStoredUnderLiteralKeys(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	r := s.run("exec", "--run-id", "at1",
		"--attr", "ticket=2056", "--attr", "eq=a=b", "--attr", "dup=first", "--attr", "dup=last",
		"--attr-json", `review.findings={"total":1}`, "--attr-json", "gone=null", "--attr-json", "big=12345678901234567890",
		"q")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	req := readJSONFile(t, filepath.Join(s.home, "runs", "at1", "request.json"))
	want := map[string]any{
		"ticket": "2056", "eq": "a=b", "dup": "last",
		"review.findings": map[string]any{"total": float64(1)},
		"gone":            nil,
		"big":             float64(12345678901234567890),
	}
	if !reflect.DeepEqual(req["attrs"], want) {
		t.Errorf("request.attrs = %v, want %v", req["attrs"], want)
	}
	if raw := readFile(t, filepath.Join(s.home, "runs", "at1", "request.json")); !strings.Contains(raw, "12345678901234567890") {
		t.Error("large integer lost precision in request.json")
	}
}

func TestAttrsEmptyObjectByDefault(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	s.run("exec", "--run-id", "at0", "q")
	req := readJSONFile(t, filepath.Join(s.home, "runs", "at0", "request.json"))
	if !reflect.DeepEqual(req["attrs"], map[string]any{}) {
		t.Errorf("request.attrs = %#v", req["attrs"])
	}
}

func TestInvalidAttrsAreUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
	}{
		{"no equals", []string{"--attr", "novalue"}, "--attr"},
		{"empty key", []string{"--attr", "=v"}, "--attr"},
		{"json no equals", []string{"--attr-json", "novalue"}, "--attr-json"},
		{"invalid json", []string{"--attr-json", "k={nope"}, "--attr-json"},
		{"trailing data", []string{"--attr-json", "k=1 2"}, "--attr-json"},
		{"empty json", []string{"--attr-json", "k="}, "--attr-json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t)
			rec := s.recordTo()
			r := s.run(append([]string{"exec"}, append(c.args, "q")...)...)
			if r.code != 2 {
				t.Fatalf("exit %d, want 2 (stderr %s)", r.code, r.stderr)
			}
			if !strings.Contains(r.stderr, c.flag) {
				t.Errorf("stderr does not name %s: %q", c.flag, r.stderr)
			}
			if exists(rec) || exists(filepath.Join(s.home, "runs")) {
				t.Error("spawned or admitted despite the usage error")
			}
		})
	}
}
