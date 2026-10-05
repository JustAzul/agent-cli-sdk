package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func indexFile(home, key string) string { return filepath.Join(home, "index", "sessions", key) }

// indexedIDs reads a session's index file as the run ids it lists, oldest first.
func indexedIDs(t *testing.T, home, key string) []string {
	t.Helper()
	b, err := os.ReadFile(indexFile(home, key))
	if err != nil {
		t.Fatalf("index %q: %v", key, err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// appendIndex writes index lines directly, as an earlier admission would have.
func appendIndex(t *testing.T, home, key string, ids ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(indexFile(home, key)), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(indexFile(home, key), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, id := range ids {
		fmt.Fprintln(f, id)
	}
}

func statusRunIDs(t *testing.T, s *sandbox, args ...string) (ids []string, r result) {
	t.Helper()
	r = s.run(append([]string{"status", "--json"}, args...)...)
	runs, _ := r.json(t)["runs"].([]any)
	for _, e := range runs {
		ids = append(ids, e.(map[string]any)["run_id"].(string))
	}
	return ids, r
}

func TestAdmissionIndexesTheRunUnderItsSession(t *testing.T) {
	cases := []struct {
		name string
		args func(s *sandbox, conv string) []string
	}{
		{"exec", func(*sandbox, string) []string { return []string{"exec", "q"} }},
		{"exec background", func(*sandbox, string) []string { return []string{"exec", "--background", "q"} }},
		{"review", func(*sandbox, string) []string { return []string{"review", "--uncommitted", "--cwd", gitRepo(t)} }},
		{"send", func(_ *sandbox, conv string) []string { return []string{"send", conv, "again"} }},
		{"send background", func(_ *sandbox, conv string) []string { return []string{"send", conv, "--background", "again"} }},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("resume-turn1"))
			conv := startConversation(t, s, "ix0", "--session-id", "S")
			s.set("FAKECODEX_FIXTURE", fixture("resume-turn2"))
			id := fmt.Sprintf("ix%d", i+1)
			args := append(c.args(s, conv), "--run-id", id, "--session-id", "S")
			if r := s.run(args...); r.code != 0 {
				t.Fatalf("%v: exit %d, stderr %s", args, r.code, r.stderr)
			}
			if w := s.run("wait", id); w.code != 0 {
				t.Fatalf("wait exit %d, stderr %s", w.code, w.stderr)
			}
			if got, want := indexedIDs(t, s.home, "S"), []string{"ix0", id}; !reflect.DeepEqual(got, want) {
				t.Errorf("index = %v, want %v", got, want)
			}
		})
	}
}

func TestRunWithoutASessionIsIndexedUnderTheFixedKey(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	if r := s.run("exec", "--run-id", "fk1", "q"); r.code != 0 {
		t.Fatalf("exit %d, stderr %s", r.code, r.stderr)
	}
	if got := indexedIDs(t, s.home, "_"); !reflect.DeepEqual(got, []string{"fk1"}) {
		t.Errorf("index _ = %v", got)
	}
	if ids, _ := statusRunIDs(t, s); !reflect.DeepEqual(ids, []string{"fk1"}) {
		t.Errorf("status with no session lists %v, want [fk1]", ids)
	}
}

func TestStatusIgnoresRunsNobodyIndexedForTheSession(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	seedRun(t, s, "idx-a", "--session-id", "X")
	// Same session, written before any index existed: there is no scan to find it.
	seedRunFilesUnindexed(t, s, "idx-legacy", nil, map[string]any{"session_id": "X"}, nil)
	ids, r := statusRunIDs(t, s, "--session-id", "X")
	if r.code != 0 || !reflect.DeepEqual(ids, []string{"idx-a"}) {
		t.Errorf("exit %d, runs %v, want only the indexed idx-a", r.code, ids)
	}
	if ids, _ := statusRunIDs(t, s, "--session-id", "no-such-session"); len(ids) != 0 {
		t.Errorf("a session without an index lists %v", ids)
	}
}

func TestStatusSkipsIndexedRunsWhoseDirectoryIsGone(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	for _, id := range []string{"gone-a", "gone-b", "gone-c"} {
		seedRun(t, s, id, "--session-id", "S")
	}
	if err := os.RemoveAll(filepath.Join(s.home, "runs", "gone-b")); err != nil {
		t.Fatal(err)
	}
	ids, r := statusRunIDs(t, s, "--session-id", "S")
	if r.code != 0 || r.stderr != "" || !reflect.DeepEqual(ids, []string{"gone-c", "gone-a"}) {
		t.Errorf("exit %d stderr %q runs %v, want [gone-c gone-a] silently", r.code, r.stderr, ids)
	}
}

// Polling cost does not grow with the runs of other sessions: run
// directories nobody indexed for this session are never opened. Their state
// files are unreadable JSON, which a scan would report as skipped runs.
func TestStatusDoesNotReadTheRunsOfOtherSessions(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	seedRun(t, s, "flat-1", "--session-id", "X")
	for i := 0; i < 200; i++ {
		dir := filepath.Join(s.home, "runs", fmt.Sprintf("decoy-%03d", i))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ids, r := statusRunIDs(t, s, "--session-id", "X")
	if r.code != 0 || r.stderr != "" || !reflect.DeepEqual(ids, []string{"flat-1"}) {
		t.Errorf("exit %d stderr %q runs %v, want [flat-1] and no warning about other runs", r.code, r.stderr, ids)
	}
}
