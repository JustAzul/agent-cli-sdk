// Command piiscan fails when a tree holds a personal email, a home-directory
// path or a UUID-shaped id that is not on the fictitious-id allowlist.
//
// Usage: piiscan [--tracked] [--allowlist FILE] [--exclude DIR]... <dir>
//
// With --tracked only files listed by `git ls-files` in <dir> are scanned;
// otherwise the whole tree is walked (skipping .git). Binary files are scanned
// through their printable strings. Exit 0: clean, 1: findings, 2: usage or I/O error.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	fset := flag.NewFlagSet("piiscan", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "usage: piiscan [--tracked] [--allowlist FILE] [--exclude DIR]... <dir>")
	}
	tracked := fset.Bool("tracked", false, "scan only files listed by git ls-files")
	allowPath := fset.String("allowlist", "", "file of allowed UUIDs (default: <dir>/testdata/pii-allowlist.txt if present)")
	var excludes stringList
	fset.Var(&excludes, "exclude", "directory (relative to <dir>) to skip; repeatable")
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fset.NArg() != 1 {
		fset.Usage()
		return 2
	}
	root := fset.Arg(0)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		fmt.Fprintf(stderr, "piiscan: %s is not a readable directory\n", root)
		return 2
	}

	if *allowPath == "" {
		def := filepath.Join(root, "testdata", "pii-allowlist.txt")
		if _, err := os.Stat(def); err == nil {
			*allowPath = def
		}
	}
	allow, err := loadAllowlist(*allowPath)
	if err != nil {
		fmt.Fprintf(stderr, "piiscan: %v\n", err)
		return 2
	}

	files, err := listFiles(root, *tracked, excludes)
	if err != nil {
		fmt.Fprintf(stderr, "piiscan: %v\n", err)
		return 2
	}

	var findings []finding
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			fmt.Fprintf(stderr, "piiscan: %v\n", err)
			return 2
		}
		findings = append(findings, scanFile(rel, data, allow)...)
	}
	if len(findings) == 0 {
		return 0
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].path != findings[j].path {
			return findings[i].path < findings[j].path
		}
		return findings[i].pos < findings[j].pos
	})
	for _, f := range findings {
		fmt.Fprintf(stdout, "%s:%s: %s: %s\n", f.path, f.where, f.kind, f.match)
	}
	fmt.Fprintf(stderr, "piiscan: %d finding(s)\n", len(findings))
	return 1
}

func loadAllowlist(path string) (map[string]bool, error) {
	allow := map[string]bool{}
	if path == "" {
		return allow, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("allowlist: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allow[strings.ToLower(line)] = true
	}
	return allow, sc.Err()
}

func excluded(rel string, excludes []string) bool {
	rel = filepath.ToSlash(rel)
	for _, e := range excludes {
		e = strings.Trim(filepath.ToSlash(e), "/")
		if rel == e || strings.HasPrefix(rel, e+"/") {
			return true
		}
	}
	return false
}

// listFiles returns the regular files to scan, as slash-free relative paths.
func listFiles(root string, tracked bool, excludes []string) ([]string, error) {
	var files []string
	if tracked {
		cmd := exec.Command("git", "-C", root, "ls-files", "-z")
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git ls-files in %s: %w", root, err)
		}
		for _, rel := range strings.Split(string(out), "\x00") {
			if rel == "" || excluded(rel, excludes) {
				continue
			}
			fi, err := os.Lstat(filepath.Join(root, rel))
			if err != nil || !fi.Mode().IsRegular() {
				continue // deleted, submodule, symlink
			}
			files = append(files, filepath.FromSlash(rel))
		}
		return files, nil
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.Name() == ".git" || (rel != "." && excluded(rel, excludes)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

func scanFile(rel string, data []byte, allow map[string]bool) []finding {
	if isBinary(data) {
		var out []finding
		for _, s := range printableStrings(data) {
			out = append(out, scanText(rel, fmt.Sprintf("@%d", s.offset), s.offset, s.text, allow)...)
		}
		return out
	}
	var out []finding
	for i, line := range bytes.Split(data, []byte("\n")) {
		out = append(out, scanText(rel, fmt.Sprintf("%d", i+1), i+1, string(line), allow)...)
	}
	return out
}
