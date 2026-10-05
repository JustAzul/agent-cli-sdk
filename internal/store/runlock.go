package store

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrRunLockHeld is returned when another process owns the run's lock.
var ErrRunLockHeld = errors.New("run lock is held by another process")

const (
	runLockWait = 500 * time.Millisecond // absorbs a concurrent reader's probe
	runLockPoll = 10 * time.Millisecond
)

// RunLock is the exclusive hold a worker keeps on its run for the run's whole
// life. Other processes see it as held; dropping it (or dying) frees it.
type RunLock struct{ f *os.File }

func (s *Store) runLockPath(runID string) string { return filepath.Join(s.RunDir(runID), "lock") }

// LockRun takes the run's exclusive lock without blocking for long, creating
// the lock file if needed. The descriptor is close-on-exec, so no child
// process inherits the lock.
func (s *Store) LockRun(runID string) (*RunLock, error) {
	f, err := os.OpenFile(s.runLockPath(runID), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(runLockWait)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &RunLock{f: f}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			f.Close()
			return nil, err
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrRunLockHeld
		}
		time.Sleep(runLockPoll)
	}
}

// Close releases the lock.
func (l *RunLock) Close() error { return l.f.Close() } // closing the descriptor drops the lock

// RunLockHeld reports, without blocking, whether some process holds the run's
// lock. A run whose lock file does not exist yet is not held.
func (s *Store) RunLockHeld(runID string) (bool, error) {
	f, err := os.OpenFile(s.runLockPath(runID), os.O_RDWR, fileMode)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		switch err {
		case nil:
			return false, nil // closing the descriptor drops the probe's hold
		case syscall.EWOULDBLOCK:
			return true, nil
		case syscall.EINTR:
		default:
			return false, err
		}
	}
}

// CancelRequestPath is the file whose presence records a request to cancel
// the run.
func (s *Store) CancelRequestPath(runID string) string {
	return filepath.Join(s.RunDir(runID), "cancel.request")
}

// WriteCancelRequest records, durably, a request to cancel the run.
func (s *Store) WriteCancelRequest(runID string, at time.Time) error {
	return writeFileAtomic(s.CancelRequestPath(runID), []byte(at.UTC().Format(time.RFC3339)+"\n"))
}

// CancelRequested reports whether a cancel request exists for the run.
func (s *Store) CancelRequested(runID string) bool {
	_, err := os.Lstat(s.CancelRequestPath(runID))
	return err == nil
}
