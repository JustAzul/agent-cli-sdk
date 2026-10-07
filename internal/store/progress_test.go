package store

import (
	"os"
	"strings"
	"testing"
	"time"
)

func progressStore(t *testing.T, content string) *Store {
	t.Helper()
	s := Open(t.TempDir())
	if err := s.CreateRunDir("r1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ProgressPath("r1"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReadProgressLeavesAnUnfinishedLastLineForLater(t *testing.T) {
	s := progressStore(t, `{"seq":0,"at":"t","kind":"command","text":"ls"}`+"\n"+`{"seq":1,"at":"t","ki`)

	entries, err := s.ReadProgress("r1", 0)

	if err != nil || len(entries) != 1 || entries[0].Text != "ls" {
		t.Errorf("entries %+v err %v, want the one finished entry", entries, err)
	}
}

func TestReadProgressReportsAMalformedLine(t *testing.T) {
	s := progressStore(t, `{"seq":0,"at":"t","kind":"command","text":"ls"}`+"\n"+"garbage\n"+`{"seq":2,"at":"t","kind":"message","text":"done"}`+"\n")

	_, err := s.ReadProgress("r1", 0)

	if err == nil || !strings.Contains(err.Error(), "progress line 2") {
		t.Errorf("err = %v, want one naming line 2", err)
	}
}

func TestProgressWriterNumbersEntriesFromZero(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.CreateRunDir("r1"); err != nil {
		t.Fatal(err)
	}
	w, err := s.OpenProgress("r1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)
	for _, text := range []string{"a", "b", "c"} {
		if err := w.Append(at, "command", text); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()

	entries, err := s.ReadProgress("r1", 1)

	if err != nil || len(entries) != 2 || entries[0].Seq != 1 || entries[0].Text != "b" || entries[1].Seq != 2 || entries[1].At != "2026-10-07T03:00:00Z" {
		t.Errorf("entries %+v err %v", entries, err)
	}
}
