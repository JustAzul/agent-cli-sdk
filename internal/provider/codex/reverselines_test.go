package codex

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func readBackwards(t *testing.T, content string, chunk int) []string {
	t.Helper()
	r := newReverseLines(strings.NewReader(content), int64(len(content)), chunk)
	var got []string
	for {
		line, err := r.next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		got = append(got, string(line))
	}
}

func TestReverseLinesYieldsEachLineNewestFirstAtEveryChunkSize(t *testing.T) {
	cases := map[string]struct {
		content string
		want    []string
	}{
		"empty file":                {"", nil},
		"one line without newline":  {"alpha", []string{"alpha"}},
		"one line with newline":     {"alpha\n", []string{"alpha"}},
		"several lines":             {"a\nbb\nccc\n", []string{"ccc", "bb", "a"}},
		"no trailing newline":       {"a\nbb\nccc", []string{"ccc", "bb", "a"}},
		"blank lines are skipped":   {"a\n\n\nb\n\n", []string{"b", "a"}},
		"a line longer than a read": {"a\n" + strings.Repeat("x", 50) + "\nb\n", []string{"b", strings.Repeat("x", 50), "a"}},
	}
	for name, c := range cases {
		for chunk := 1; chunk <= 12; chunk++ {
			got := readBackwards(t, c.content, chunk)
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Errorf("%s, chunk %d: got %q, want %q", name, chunk, got, c.want)
			}
		}
	}
}

func TestReverseLinesReadsOnlyWhatItIsAskedFor(t *testing.T) {
	content := strings.Repeat("old line of the file\n", 1000) + "last\n"
	counted := &countingReader{r: strings.NewReader(content)}
	r := newReverseLines(counted, int64(len(content)), 64)

	line, err := r.next()

	if err != nil || string(line) != "last" {
		t.Fatalf("next = %q, %v", line, err)
	}
	if counted.n > 64 {
		t.Errorf("read %d bytes for the last line, want at most one chunk (64)", counted.n)
	}
}

type countingReader struct {
	r *strings.Reader
	n int
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n += n
	return n, err
}

// shrunkReader reports a size larger than what it still holds, as a file does
// that was truncated after it was opened: the read comes up short with io.EOF.
type shrunkReader struct{ data string }

func (s shrunkReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestReverseLinesFailsOnAShortReadInsteadOfReturningUnreadBytes(t *testing.T) {
	r := newReverseLines(shrunkReader{data: "first\nsecond\n"}, int64(len("first\nsecond\n"))+20, 64)

	line, err := r.next()

	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("next = %q, %v, want io.ErrUnexpectedEOF", line, err)
	}
}

type failingReader struct{ err error }

func (f failingReader) ReadAt([]byte, int64) (int, error) { return 0, f.err }

func TestReverseLinesReturnsTheReadError(t *testing.T) {
	boom := errors.New("disk gone")
	r := newReverseLines(failingReader{boom}, 10, 64)

	if _, err := r.next(); !errors.Is(err, boom) {
		t.Errorf("next error = %v, want %v", err, boom)
	}
}
