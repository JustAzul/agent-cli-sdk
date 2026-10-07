package e2e

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func progressEntries(t *testing.T, m map[string]any) []string {
	t.Helper()
	raw, _ := m["entries"].([]any)
	out := []string{}
	for _, e := range raw {
		entry := e.(map[string]any)
		out = append(out, entry["kind"].(string)+": "+entry["text"].(string))
	}
	return out
}

func TestProgressRecordsWhatTheProviderDidInOrder(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("review-ok"))
	if r := s.run("exec", "--run-id", "pg1", "q"); r.code != 0 {
		t.Fatalf("exec exit %d %s", r.code, r.stderr)
	}

	r := s.run("progress", "pg1", "--json")

	if r.code != 0 {
		t.Fatalf("progress exit %d %s", r.code, r.stderr)
	}
	got := r.json(t)
	want := []string{
		"command: git status --short",
		"command: git status --short",
		"message: There are no tracked-file changes, and the untracked files do not contain reviewable code changes. No actionable findings identified.",
	}
	if entries := progressEntries(t, got); !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %q, want %q", entries, want)
	}
	if got["next"] != float64(3) || got["state"] != "done" || got["run_id"] != "pg1" {
		t.Errorf("next=%v state=%v run_id=%v", got["next"], got["state"], got["run_id"])
	}
	info, err := os.Stat(filepath.Join(s.home, "runs", "pg1", "progress.jsonl"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("progress.jsonl mode = %v (err %v), want 0600", info.Mode().Perm(), err)
	}
}

func TestProgressFromSkipsWhatWasAlreadyRead(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("review-ok"))
	s.run("exec", "--run-id", "pg2", "q")

	later := s.run("progress", "pg2", "--from", "2", "--json").json(t)
	none := s.run("progress", "pg2", "--from", "3", "--json").json(t)

	if entries := progressEntries(t, later); len(entries) != 1 || !strings.HasPrefix(entries[0], "message: ") {
		t.Errorf("--from 2 entries = %q", entries)
	}
	if entries := progressEntries(t, none); len(entries) != 0 || none["next"] != float64(3) {
		t.Errorf("--from 3 entries = %q next = %v", entries, none["next"])
	}
}

func TestProgressPlainOutputIsOneLinePerEntry(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	s.run("exec", "--run-id", "pg3", "q")

	r := s.run("progress", "pg3")

	if r.code != 0 || r.stdout != "message: pong\n" {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestProgressErrors(t *testing.T) {
	s := newSandbox(t)
	if r := s.run("progress", "r-missing", "--json"); r.code != 4 {
		t.Errorf("unknown run: exit %d, want 4", r.code)
	}
	if r := s.run("progress"); r.code != 2 {
		t.Errorf("no run id: exit %d, want 2", r.code)
	}
	if r := s.run("progress", "r-x", "--from", "-1"); r.code != 2 {
		t.Errorf("negative --from: exit %d, want 2", r.code)
	}
}
