package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ProgressEntry is one line of a run's progress file: something the provider
// did while the run worked, in the order it happened.
type ProgressEntry struct {
	Seq  int    `json:"seq"`
	At   string `json:"at"`
	Kind string `json:"kind"` // command, message or reasoning
	Text string `json:"text"`
}

// ProgressPath is the file a run's progress entries are appended to.
func (s *Store) ProgressPath(runID string) string {
	return filepath.Join(s.RunDir(runID), "progress.jsonl")
}

// ProgressWriter appends one run's progress entries, numbering them from 0.
// One runner owns it; it is not safe for concurrent use.
type ProgressWriter struct {
	f   *os.File
	seq int
}

// OpenProgress creates the run's progress file with user-only permissions.
// The run directory must exist.
func (s *Store) OpenProgress(runID string) (*ProgressWriter, error) {
	f, err := os.OpenFile(s.ProgressPath(runID), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return nil, err
	}
	return &ProgressWriter{f: f}, nil
}

// Append writes one entry as a single line, so a reader never sees half of it.
func (w *ProgressWriter) Append(at time.Time, kind, text string) error {
	line, err := json.Marshal(ProgressEntry{Seq: w.seq, At: at.UTC().Format(time.RFC3339), Kind: kind, Text: text})
	if err != nil {
		return err
	}
	if _, err := w.f.Write(append(line, '\n')); err != nil {
		return err
	}
	w.seq++
	return nil
}

func (w *ProgressWriter) Close() error { return w.f.Close() }

// ReadProgress returns the run's progress entries numbered from `from` on. A
// run with no progress file yet has none. A last line with no newline yet is
// still being written and is left for the next read; any other line that is
// not an entry is an error, never skipped.
func (s *Store) ReadProgress(runID string, from int) ([]ProgressEntry, error) {
	data, err := os.ReadFile(s.ProgressPath(runID))
	if errors.Is(err, os.ErrNotExist) {
		return []ProgressEntry{}, nil
	}
	if err != nil {
		return nil, err
	}

	lines := bytes.Split(data, []byte{'\n'})
	complete := lines[:len(lines)-1]
	entries := []ProgressEntry{}
	for i, line := range complete {
		var e ProgressEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("progress line %d is not an entry: %w", i+1, err)
		}
		if e.Seq >= from {
			entries = append(entries, e)
		}
	}
	return entries, nil
}
