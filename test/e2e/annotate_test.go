package e2e

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// seedRun runs a fake exec so the home holds one finished run.
func seedRun(t *testing.T, s *sandbox, runID string, extra ...string) {
	t.Helper()
	s.set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	args := append([]string{"exec", "--run-id", runID}, extra...)
	if r := s.run(append(args, "q")...); r.code != 0 {
		t.Fatalf("seed exec exit %d: %s", r.code, r.stderr)
	}
}

func annotations(t *testing.T, home string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, rec := range telemetryRecords(t, home) {
		if rec["kind"] == "annotation" {
			out = append(out, rec)
		}
	}
	return out
}

func TestAnnotateAppendsAnAnnotationRecord(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "an1")
	r := s.run("annotate", "an1", "--attr", "acted=yes", "--attr-json", `review.findings={"total":2}`, "--attr-json", "big=12345678901234567890")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	anns := annotations(t, s.home)
	if len(anns) != 1 {
		t.Fatalf("annotations = %v", anns)
	}
	a := anns[0]
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"attrs", "kind", "run_id", "ts", "v"}) {
		t.Errorf("annotation keys = %v", keys)
	}
	want := map[string]any{"acted": "yes", "review.findings": map[string]any{"total": float64(2)}, "big": float64(12345678901234567890)}
	if a["v"] != float64(1) || a["run_id"] != "an1" || !reflect.DeepEqual(a["attrs"], want) {
		t.Errorf("annotation = %v", a)
	}
	if ts, _ := a["ts"].(string); !strings.HasSuffix(ts, "Z") || len(ts) != len("2026-10-04T23:15:00Z") {
		t.Errorf("ts = %q", a["ts"])
	}
	if !exists(currentMonthFile(s.home)) {
		t.Error("annotation not in the current month file")
	}
	if raw := readFile(t, currentMonthFile(s.home)); !strings.Contains(raw, "12345678901234567890") {
		t.Error("large integer lost precision")
	}
}

func TestAnnotateByOutputPath(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "ap1")
	out := filepath.Join(s.home, "runs", "ap1", "output.md")
	if r := s.run("annotate", out, "--attr", "k=v"); r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	anns := annotations(t, s.home)
	if len(anns) != 1 || anns[0]["run_id"] != "ap1" {
		t.Errorf("annotations = %v", anns)
	}
}

func TestAnnotateByOutputPathThroughASymlinkedDirectory(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "sl1")
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(s.home, link); err != nil {
		t.Fatal(err)
	}
	if r := s.run("annotate", filepath.Join(link, "runs", "sl1", "output.md"), "--attr", "k=v"); r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	anns := annotations(t, s.home)
	if len(anns) != 1 || anns[0]["run_id"] != "sl1" {
		t.Errorf("annotations = %v", anns)
	}
}

func TestAnnotateMatchesWhenTheHomeItselfIsReachedThroughASymlink(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real-home")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s := newSandbox(t)
	s.home = link
	seedRun(t, s, "sl2")
	resolved, err := filepath.EvalSymlinks(filepath.Join(real, "runs", "sl2", "output.md"))
	if err != nil {
		t.Fatal(err)
	}
	if r := s.run("annotate", resolved, "--attr", "k=v"); r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if anns := annotations(t, real); len(anns) != 1 || anns[0]["run_id"] != "sl2" {
		t.Errorf("annotations = %v", anns)
	}
}

func TestAnnotateUnknownRunExits4(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "an2")
	for _, target := range []string{"nope", "../runs/an2", filepath.Join(s.home, "runs", "nope", "output.md"), "/nonexistent/output.md"} {
		r := s.run("annotate", target, "--attr", "k=v")
		if r.code != 4 {
			t.Errorf("annotate %q: exit %d, want 4 (stderr %q)", target, r.code, r.stderr)
		}
	}
	if len(annotations(t, s.home)) != 0 {
		t.Error("an annotation was written for an unknown run")
	}
	r := s.run("annotate", "nope", "--attr", "k=v", "--json")
	if r.code != 4 || r.json(t)["sdk_status"] != "not_found" {
		t.Errorf("json error: exit %d stdout %q", r.code, r.stdout)
	}
}

func TestAnnotateUsageErrors(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "an3")
	cases := map[string][]string{
		"no attrs":      {"annotate", "an3"},
		"no target":     {"annotate", "--attr", "k=v"},
		"two targets":   {"annotate", "an3", "an3", "--attr", "k=v"},
		"bad attr":      {"annotate", "an3", "--attr", "novalue"},
		"bad attr json": {"annotate", "an3", "--attr-json", "k={nope"},
	}
	for name, args := range cases {
		if r := s.run(args...); r.code != 2 {
			t.Errorf("%s: exit %d, want 2 (stderr %q)", name, r.code, r.stderr)
		}
	}
	if len(annotations(t, s.home)) != 0 {
		t.Error("an annotation was written despite a usage error")
	}
}

func TestAnnotateJSONSuccessObject(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "an4")
	r := s.run("annotate", "an4", "--attr", "k=v", "--json")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	j := r.json(t)
	if j["run_id"] != "an4" || j["sdk_status"] != "ok" || !reflect.DeepEqual(j["attrs"], map[string]any{"k": "v"}) {
		t.Errorf("json = %v", j)
	}
}
