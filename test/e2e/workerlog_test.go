package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workerLogPath(s *sandbox, runID string) string {
	return filepath.Join(s.home, "runs", runID, "worker.log")
}

func assertUserOnlyFile(t *testing.T, path string) {
	t.Helper()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("%s: %v, %v; want a 0600 file", path, info, err)
	}
}

// A job that runs cleanly leaves an empty worker log.
func TestWorkerLogExistsAndIsEmptyForACleanJob(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	admitJob(t, s, "wl1", "q")
	waitOK(t, s, "wl1")
	assertUserOnlyFile(t, workerLogPath(s, "wl1"))
	if got := readFile(t, workerLogPath(s, "wl1")); got != "" {
		t.Errorf("worker.log = %q, want it empty", got)
	}
}

// What the runner warns about in a job is kept, not thrown away.
func TestWorkerLogKeepsRunnerWarnings(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok")).set("FAKECODEX_SLEEP_MS", "1500")
	admitJob(t, s, "wl2", "q")
	// stderr.tail cannot be replaced by a file once a directory sits there.
	if err := os.Mkdir(filepath.Join(s.home, "runs", "wl2", "stderr.tail"), 0o700); err != nil {
		t.Fatal(err)
	}
	waitOK(t, s, "wl2")
	assertUserOnlyFile(t, workerLogPath(s, "wl2"))
	if got := readFile(t, workerLogPath(s, "wl2")); !strings.Contains(got, "could not write stderr.tail") {
		t.Errorf("worker.log = %q, want the warning about stderr.tail", got)
	}
}
