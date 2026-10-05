package main

import (
	"regexp"
	"strings"
)

type finding struct {
	path  string
	where string // "<line>" for text, "@<offset>" for binary strings
	pos   int
	kind  string
	match string
}

var (
	emailRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,})`)
	pathRE  = regexp.MustCompile(`/(?:home|Users)/[A-Za-z0-9._-]+`)
	uuidRE  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

var allowedEmailDomains = map[string]bool{
	"users.noreply.github.com": true,
	"example.com":              true,
	"example.org":              true,
}

func scanText(path, where string, pos int, text string, allow map[string]bool) []finding {
	var out []finding
	for _, m := range emailRE.FindAllStringSubmatch(text, -1) {
		if !allowedEmailDomains[strings.ToLower(m[1])] {
			out = append(out, finding{path, where, pos, "email", m[0]})
		}
	}
	for _, m := range pathRE.FindAllString(text, -1) {
		out = append(out, finding{path, where, pos, "home-path", m})
	}
	for _, m := range uuidRE.FindAllString(text, -1) {
		if !allow[strings.ToLower(m)] {
			out = append(out, finding{path, where, pos, "uuid", m})
		}
	}
	return out
}

// isBinary reports whether data holds a NUL byte near its start.
func isBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return false
}

type extracted struct {
	offset int
	text   string
}

const minStringLen = 6

// printableStrings returns runs of printable ASCII of at least minStringLen
// bytes, like strings(1).
func printableStrings(data []byte) []extracted {
	var out []extracted
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= minStringLen {
			out = append(out, extracted{start, string(data[start:end])})
		}
		start = -1
	}
	for i, b := range data {
		if (b >= 0x20 && b <= 0x7e) || b == '\t' {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(data))
	return out
}
