package runner

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
)

const excerptMaxRunes = 200

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// errorExcerpt returns the last error event message, or when none
// exists and the exit is non-zero the last non-empty stderr line, stripped of
// ANSI escapes and truncated to 200 characters.
func errorExcerpt(eventError string, exit int, stderrLine string) string {
	s := eventError
	if s == "" && exit != 0 {
		s = stderrLine
	}
	s = strings.TrimSpace(ansiEscape.ReplaceAllString(s, ""))
	if r := []rune(s); len(r) > excerptMaxRunes {
		s = string(r[:excerptMaxRunes])
	}
	return s
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) Bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.buf)
}
