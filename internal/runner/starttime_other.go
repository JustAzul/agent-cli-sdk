//go:build !linux && !darwin

package runner

import "errors"

// StartTime is not available on this platform.
func StartTime(pid int) (string, error) {
	return "", errors.New("process start time is not supported on this platform")
}
