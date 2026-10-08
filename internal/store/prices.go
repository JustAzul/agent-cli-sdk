package store

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// ErrPricesLockHeld is returned when another process is refreshing the price
// cache on this home.
var ErrPricesLockHeld = errors.New("a price refresh is already running")

func (s *Store) pricesLockPath() string { return filepath.Join(s.Home, "prices.lock") }
func (s *Store) pricesPath() string     { return filepath.Join(s.Home, "prices.json") }

// LockPrices takes the home's price-refresh lock without waiting and returns
// the function releasing it. The home is created when missing. A lock already
// held yields ErrPricesLockHeld. The descriptor is close-on-exec.
func (s *Store) LockPrices() (func(), error) {
	if err := os.MkdirAll(s.Home, dirMode); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.pricesLockPath(), os.O_RDWR|os.O_CREATE, fileMode)
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
		err = ErrPricesLockHeld
	}
	f.Close()
	return nil, err
}

// ReadPrices returns the bytes of the price cache. A missing file yields an
// error satisfying errors.Is(err, os.ErrNotExist).
func (s *Store) ReadPrices() ([]byte, error) {
	return os.ReadFile(s.pricesPath())
}

// WritePrices replaces the price cache atomically, creating the home when
// missing.
func (s *Store) WritePrices(data []byte) error {
	if err := os.MkdirAll(s.Home, dirMode); err != nil {
		return err
	}
	return writeFileAtomic(s.pricesPath(), data)
}
