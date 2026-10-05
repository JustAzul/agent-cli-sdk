package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ErrPruneLockHeld is returned when another process is running an automatic
// prune on this home.
var ErrPruneLockHeld = errors.New("an automatic prune is already running")

func (s *Store) pruneLockPath() string  { return filepath.Join(s.Home, "prune.lock") }
func (s *Store) pruneStampPath() string { return filepath.Join(s.Home, "prune.stamp") }

// LockPrune takes the home's automatic-prune lock without waiting and returns
// the function releasing it. A lock already held yields ErrPruneLockHeld. The
// descriptor is close-on-exec.
func (s *Store) LockPrune() (func(), error) {
	f, err := os.OpenFile(s.pruneLockPath(), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	switch err {
	case nil:
		return func() { f.Close() }, nil // closing the descriptor drops the lock
	case syscall.EWOULDBLOCK:
		err = ErrPruneLockHeld
	}
	f.Close()
	return nil, err
}

// PruneStamp is the time the last automatic prune started. It reports false
// when there is no stamp or it cannot be read as a time.
func (s *Store) PruneStamp() (time.Time, bool) {
	data, err := os.ReadFile(s.pruneStampPath())
	if err != nil {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	return at, err == nil
}

// WritePruneStamp records at as the start of the latest automatic prune.
func (s *Store) WritePruneStamp(at time.Time) error {
	return writeFileAtomic(s.pruneStampPath(), []byte(at.UTC().Format(time.RFC3339)+"\n"))
}
