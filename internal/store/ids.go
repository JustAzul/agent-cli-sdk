package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

const idTimeLayout = "20060102T150405Z"

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// NewRunID returns a generated run id: r-<UTC yyyymmddThhmmssZ>-<8 hex>.
func NewRunID(now time.Time) string { return newID("r", now) }

// NewConversationID returns a generated conversation id: c-<same shape>.
func NewConversationID(now time.Time) string { return newID("c", now) }

func newID(prefix string, now time.Time) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("store: no randomness: %v", err))
	}
	return prefix + "-" + now.UTC().Format(idTimeLayout) + "-" + hex.EncodeToString(b[:])
}

// ValidateRunID checks a caller-supplied run id.
func ValidateRunID(id string) error {
	if !runIDPattern.MatchString(id) {
		return fmt.Errorf("run id must match [A-Za-z0-9._-]{1,128}")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("run id must not be %q", id)
	}
	if id[0] == '-' || id[0] == '.' {
		return fmt.Errorf("run id must not start with %q", string(id[0]))
	}
	return nil
}
