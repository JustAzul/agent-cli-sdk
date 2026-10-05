package store

import (
	"io"
	"os"
	"path/filepath"
	"sync"
)

// WorkerLogLimit is how much of the end of a worker's log is kept, the same
// bound stderr.tail has.
const WorkerLogLimit = 64 * 1024

// WorkerLogPath is the file a job's worker writes its diagnostics to.
func (s *Store) WorkerLogPath(runID string) string {
	return filepath.Join(s.RunDir(runID), "worker.log")
}

// OpenWorkerLog opens the run's worker log for appending, creating it with
// user-only permissions. The run directory must exist.
func (s *Store) OpenWorkerLog(runID string) (*os.File, error) {
	return os.OpenFile(s.WorkerLogPath(runID), os.O_RDWR|os.O_CREATE|os.O_APPEND, fileMode)
}

// tailFile is a writer that appends to a file and keeps only the last max
// bytes of everything written through it.
type tailFile struct {
	mu  sync.Mutex
	f   *os.File
	max int
}

// NewTailWriter returns a writer that appends to f (opened for reading and
// appending) while keeping the file at no more than max bytes: the oldest
// bytes are dropped first.
func NewTailWriter(f *os.File, max int) io.Writer { return &tailFile{f: f, max: max} }

func (t *tailFile) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	if len(p) > t.max {
		p = p[len(p)-t.max:]
	}
	info, err := t.f.Stat()
	if err != nil {
		return 0, err
	}
	keep := int64(t.max - len(p))
	if info.Size() > keep {
		data, err := t.tailOf(info.Size(), keep)
		if err != nil {
			return 0, err
		}
		p = append(data, p...)
	}
	if _, err := t.f.Write(p); err != nil {
		return 0, err
	}
	return n, nil
}

// tailOf reads the last keep bytes of the file and empties it, ready for the
// bytes to be written back after the new ones are added.
func (t *tailFile) tailOf(size, keep int64) ([]byte, error) {
	data := make([]byte, keep)
	if keep > 0 {
		if _, err := t.f.ReadAt(data, size-keep); err != nil {
			return nil, err
		}
	}
	return data, t.f.Truncate(0)
}
