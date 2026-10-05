package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// emptySessionKey names the index of runs dispatched without a session id.
const emptySessionKey = "_"

func (s *Store) indexDir() string        { return filepath.Join(s.Home, "index") }
func (s *Store) sessionIndexDir() string { return filepath.Join(s.indexDir(), "sessions") }

// sessionIndexPath is the index file of a session. A session id that is a
// safe file name is the file name; an empty one uses a fixed key; any other id
// (a path separator, a leading dot, an over-long id, the fixed key itself)
// maps to a digest, so a caller-supplied id never leaves the index directory
// or shares a file with another session.
func (s *Store) sessionIndexPath(sessionID string) string {
	key := sessionID
	switch {
	case sessionID == "":
		key = emptySessionKey
	case sessionID == emptySessionKey || sessionID[0] == '.' || !runIDPattern.MatchString(sessionID):
		sum := sha256.Sum256([]byte(sessionID))
		key = "~" + hex.EncodeToString(sum[:16])
	}
	return filepath.Join(s.sessionIndexDir(), key)
}

// lockIndex takes the exclusive lock that orders index appends against index
// rewrites and removals, and returns the function releasing it. The
// descriptor is close-on-exec.
func (s *Store) lockIndex() (func(), error) {
	if err := os.MkdirAll(s.sessionIndexDir(), dirMode); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.indexDir(), "lock"), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil // closing the descriptor drops the lock
}

// IndexRun appends runID to the index of the session that dispatched it. The
// append is one write of "<run id>\n" to a file opened for append, made under
// the index lock so a prune rewriting or removing the file never loses it.
func (s *Store) IndexRun(sessionID, runID string) error {
	unlock, err := s.lockIndex()
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.OpenFile(s.sessionIndexPath(sessionID), os.O_WRONLY|os.O_APPEND|os.O_CREATE, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(runID + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// SessionRunIDs lists the run ids indexed for the session, newest first, each
// once. A session with no index has none. Nothing but that one file is read.
func (s *Store) SessionRunIDs(sessionID string) ([]string, error) {
	data, err := os.ReadFile(s.sessionIndexPath(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return newestFirst(data), nil
}

func newestFirst(data []byte) []string {
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	seen := make(map[string]bool, len(lines))
	ids := make([]string, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		id := string(lines[i])
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}
