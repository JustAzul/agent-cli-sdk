// Package store owns the on-disk layout: home resolution, run directories,
// state files and conversation records.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// ErrRunExists is returned when a run directory already exists.
var ErrRunExists = errors.New("run already exists")

// ResolveHome applies AGENTCLI_HOME, then $XDG_STATE_HOME/agentcli, then
// ~/.local/state/agentcli.
func ResolveHome(getenv func(string) string) (string, error) {
	if h := getenv("AGENTCLI_HOME"); h != "" {
		return h, nil
	}
	if x := getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "agentcli"), nil
	}
	if h := getenv("HOME"); h != "" {
		return filepath.Join(h, ".local", "state", "agentcli"), nil
	}
	return "", errors.New("cannot resolve the agentcli home: set AGENTCLI_HOME")
}

// Store is the agentcli home directory.
type Store struct{ Home string }

// Open returns a store rooted at home. It touches nothing on disk.
func Open(home string) *Store { return &Store{Home: home} }

// RunDir is the directory of one run.
func (s *Store) RunDir(runID string) string { return filepath.Join(s.Home, "runs", runID) }

// OutputPath is the provider's final-message file for a run.
func (s *Store) OutputPath(runID string) string { return filepath.Join(s.RunDir(runID), "output.md") }

// RunExists reports whether a run directory exists.
func (s *Store) RunExists(runID string) bool {
	_, err := os.Lstat(s.RunDir(runID))
	return err == nil
}

// CreateRunDir creates runs/<id> and fails with ErrRunExists if it is present.
func (s *Store) CreateRunDir(runID string) error {
	if err := os.MkdirAll(filepath.Join(s.Home, "runs"), dirMode); err != nil {
		return err
	}
	if err := os.Mkdir(s.RunDir(runID), dirMode); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrRunExists
		}
		return err
	}
	return nil
}

func (s *Store) writeRunFile(runID, name string, data []byte) error {
	return writeFileAtomic(filepath.Join(s.RunDir(runID), name), data)
}

// WritePrompt stores the full prompt.
func (s *Store) WritePrompt(runID string, prompt []byte) error {
	return s.writeRunFile(runID, "prompt.md", prompt)
}

// WriteRequest stores the resolved request.
func (s *Store) WriteRequest(runID string, request any) error {
	return s.writeJSON(filepath.Join(s.RunDir(runID), "request.json"), request)
}

// WriteStderrTail stores the tail of the provider's stderr.
func (s *Store) WriteStderrTail(runID string, tail []byte) error {
	return s.writeRunFile(runID, "stderr.tail", tail)
}

// ReadRequest decodes the run's request.json into v.
func (s *Store) ReadRequest(runID string, v any) error {
	return readJSON(filepath.Join(s.RunDir(runID), "request.json"), v)
}

// WriteState atomically replaces the run's state file.
func (s *Store) WriteState(runID string, st State) error {
	return s.writeJSON(filepath.Join(s.RunDir(runID), "state.json"), st)
}

// ReadState loads the run's state file.
func (s *Store) ReadState(runID string) (State, error) {
	var st State
	err := readJSON(filepath.Join(s.RunDir(runID), "state.json"), &st)
	return st, err
}

func (s *Store) conversationsDir() string { return filepath.Join(s.Home, "conversations") }

func (s *Store) conversationPath(id string) string {
	return filepath.Join(s.conversationsDir(), id+".json")
}

func (s *Store) conversationLockPath(id string) string {
	return filepath.Join(s.conversationsDir(), id+".lock")
}

// CreateConversation writes a new conversation record.
func (s *Store) CreateConversation(c Conversation) error {
	if err := os.MkdirAll(filepath.Join(s.Home, "conversations"), dirMode); err != nil {
		return err
	}
	return s.writeJSON(s.conversationPath(c.ConversationID), c)
}

// ReadConversation loads a conversation record.
func (s *Store) ReadConversation(id string) (Conversation, error) {
	var c Conversation
	err := readJSON(s.conversationPath(id), &c)
	return c, err
}

// UpdateConversation read-modify-writes a conversation record under the
// conversation lock, so it never interleaves with an admission.
func (s *Store) UpdateConversation(id string, mutate func(*Conversation)) error {
	unlock, err := s.lockConversation(id)
	if err != nil {
		return err
	}
	defer unlock()
	c, err := s.ReadConversation(id)
	if err != nil {
		return err
	}
	mutate(&c)
	return s.writeJSON(s.conversationPath(id), c)
}

func (s *Store) writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeFileAtomic writes via a temp file in the same directory plus rename.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(err)
	}
	if err := tmp.Chmod(fileMode); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		return cleanup(err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
