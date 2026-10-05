//go:build linux

package runner

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// StartTime returns an opaque string identifying when the process started: the
// same pid at a later time is a different process. It is the start time in
// clock ticks since boot, field 22 of /proc/<pid>/stat.
func StartTime(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	// The process name (field 2) may hold spaces and parentheses; the fields
	// that follow start after the last closing parenthesis, at field 3.
	text := string(data)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return "", fmt.Errorf("unexpected /proc/%d/stat format", pid)
	}
	fields := strings.Fields(text[end+1:])
	const startTimeField = 22 - 3
	if len(fields) <= startTimeField {
		return "", fmt.Errorf("/proc/%d/stat has %d fields after the name, want more than %d", pid, len(fields), startTimeField)
	}
	if _, err := strconv.ParseUint(fields[startTimeField], 10, 64); err != nil {
		return "", fmt.Errorf("/proc/%d/stat start time %q: %w", pid, fields[startTimeField], err)
	}
	return fields[startTimeField], nil
}
