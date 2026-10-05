package telemetry

import (
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lock is an exclusive hold on telemetry/.lock. While it is held, Append and
// AppendLine from other processes wait; callers that already hold a Lock must
// write through it rather than call them, or they would wait on themselves.
type Lock struct {
	home string
	f    *os.File
}

// AcquireLock creates the telemetry directory if needed and takes the lock,
// waiting at most wait (LockWait when wait is not positive).
func AcquireLock(home string, wait time.Duration) (*Lock, error) {
	dir := filepath.Join(home, "telemetry")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, err
	}
	if wait <= 0 {
		wait = LockWait
	}
	if err := lockExclusive(f, wait); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{home: home, f: f}, nil
}

// Close releases the lock.
func (l *Lock) Close() error {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// AppendLines writes each line (without its newline) to the month file of
// at's UTC month in one write call, preceded by a newline when the file ends
// mid-line.
func (l *Lock) AppendLines(at time.Time, lines ...[]byte) error {
	var buf []byte
	for _, line := range lines {
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	path := filepath.Join(l.home, "telemetry", at.UTC().Format("2006-01")+".jsonl")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, fileMode)
	if err != nil {
		return err
	}
	defer f.Close()
	torn, err := endsMidLine(f)
	if err != nil {
		return err
	}
	if torn {
		buf = append([]byte{'\n'}, buf...)
	}
	_, err = f.Write(buf)
	return err
}

// AppendLine appends one pre-encoded record under the lock, like Append does
// for a Record.
func AppendLine(home string, line []byte, at time.Time, opt Options) error {
	l, err := AcquireLock(home, opt.LockWait)
	if err != nil {
		return err
	}
	defer l.Close()
	return l.AppendLines(at, line)
}
