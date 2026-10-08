package codex

import (
	"bytes"
	"errors"
	"io"
)

// reverseLines yields the non-empty lines of a file from its end, reading it
// in chunks backwards so that a caller who stops early reads only the tail.
// A line is returned whole however long it is (a large tool output spans many
// chunks).
type reverseLines struct {
	src   io.ReaderAt
	pos   int64  // everything before pos is still unread
	chunk int    // bytes to read at a time
	rest  []byte // read but not yet returned; its last line may be incomplete
}

func newReverseLines(src io.ReaderAt, size int64, chunk int) *reverseLines {
	return &reverseLines{src: src, pos: size, chunk: chunk}
}

// next returns the previous line without its newline, or io.EOF once the
// start of the file has been passed. The slice is valid until the next call.
func (r *reverseLines) next() ([]byte, error) {
	var later [][]byte // pieces of the line being assembled, the latest first
	for {
		if i := bytes.LastIndexByte(r.rest, '\n'); i >= 0 {
			later = append(later, r.rest[i+1:])
			r.rest = r.rest[:i]
			if line := joinReversed(later); len(line) > 0 {
				return line, nil
			}
			later = later[:0]
			continue
		}
		if len(r.rest) > 0 {
			later = append(later, r.rest)
			r.rest = nil
		}
		if r.pos == 0 {
			if line := joinReversed(later); len(line) > 0 {
				return line, nil
			}
			return nil, io.EOF
		}
		if err := r.readPrevious(); err != nil {
			return nil, err
		}
	}
}

// readPrevious reads the chunk before the unread position into a fresh buffer
// (earlier pieces of a line still point into the previous ones).
func (r *reverseLines) readPrevious() error {
	n := int64(r.chunk)
	if n > r.pos {
		n = r.pos
	}
	buf := make([]byte, n)
	got, err := r.src.ReadAt(buf, r.pos-n)
	if got < len(buf) {
		// A short read means the file shrank since its size was taken: what
		// is missing must never pass for content.
		if err == nil || errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return err
	}
	r.pos -= n
	r.rest = buf
	return nil
}

// joinReversed concatenates pieces given latest first into one line.
func joinReversed(pieces [][]byte) []byte {
	if len(pieces) == 1 {
		return pieces[0]
	}
	size := 0
	for _, p := range pieces {
		size += len(p)
	}
	line := make([]byte, 0, size)
	for i := len(pieces) - 1; i >= 0; i-- {
		line = append(line, pieces[i]...)
	}
	return line
}
