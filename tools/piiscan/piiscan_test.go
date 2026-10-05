package main_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var scanBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "piiscan-*")
	if err != nil {
		panic(err)
	}
	scanBin = filepath.Join(tmp, "piiscan")
	cmd := exec.Command("go", "build", "-o", scanBin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.RemoveAll(tmp)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

type out struct {
	stdout, stderr string
	code           int
}

func scan(t *testing.T, args ...string) out {
	t.Helper()
	cmd := exec.Command(scanBin, args...)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return out{o.String(), e.String(), code}
}

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Offending samples are assembled at run time so no literal personal email,
// home path or unlisted id exists in any tracked file.
func webmail() string  { return "alice" + "@" + "gmail" + ".com" }
func homePath() string { return "/" + "home" + "/" + "alice" + "/work" }
func macPath() string  { return "/" + "Users" + "/" + "alice.b" + "/Library" }
func strayID() string  { return "7f3a9c12-" + "55d0-4b7e-" + "9a31-" + "0c8d2e6b4f17" }

func allowlist(t *testing.T, ids ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(p, []byte("# fictitious ids\n"+strings.Join(ids, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFlagsPersonalEmail(t *testing.T) {
	dir := tree(t, map[string]string{"notes/a.md": "line one\ncontact: " + webmail() + "\n"})
	r := scan(t, "--allowlist", allowlist(t), dir)
	if r.code != 1 {
		t.Fatalf("exit %d, want 1\n%s%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout+r.stderr, filepath.Join("notes", "a.md")+":2:") {
		t.Errorf("finding does not list file:line (want notes/a.md:2):\n%s%s", r.stdout, r.stderr)
	}
}

func TestAllowsNoreplyAndExampleEmails(t *testing.T) {
	dir := tree(t, map[string]string{"a.txt": strings.Join([]string{
		"12345+someone@users.noreply.github.com",
		"dev@example.com", "ops@example.org", "pkg@v1.2.3",
	}, "\n")})
	if r := scan(t, "--allowlist", allowlist(t), dir); r.code != 0 {
		t.Errorf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestFlagsHomePaths(t *testing.T) {
	for name, sample := range map[string]string{"linux": homePath(), "mac": macPath()} {
		t.Run(name, func(t *testing.T) {
			dir := tree(t, map[string]string{"cfg.json": "{\n \"path\": \"" + sample + "\"\n}\n"})
			r := scan(t, "--allowlist", allowlist(t), dir)
			if r.code != 1 || !strings.Contains(r.stdout+r.stderr, "cfg.json:2:") {
				t.Errorf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
			}
		})
	}
}

func TestAllowsPlaceholderPaths(t *testing.T) {
	dir := tree(t, map[string]string{"doc.md": "see /home/<name>/x, /Users/<user>/y, /home/ and ~/home\n"})
	if r := scan(t, "--allowlist", allowlist(t), dir); r.code != 0 {
		t.Errorf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestFlagsUUIDsOffTheAllowlist(t *testing.T) {
	listed := "00000000-0000-4000-8000-000000000001"
	dir := tree(t, map[string]string{"a.jsonl": "{\"id\":\"" + listed + "\"}\n{\"id\":\"" + strayID() + "\"}\n"})
	r := scan(t, "--allowlist", allowlist(t, listed), dir)
	if r.code != 1 || !strings.Contains(r.stdout+r.stderr, "a.jsonl:2:") {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if strings.Contains(r.stdout+r.stderr, "a.jsonl:1:") {
		t.Errorf("allowlisted id was flagged:\n%s%s", r.stdout, r.stderr)
	}
	// Allowlist matching ignores case.
	dir = tree(t, map[string]string{"b.txt": strings.ToUpper(listed)})
	if r := scan(t, "--allowlist", allowlist(t, listed), dir); r.code != 0 {
		t.Errorf("upper-case allowlisted id flagged: %s%s", r.stdout, r.stderr)
	}
}

func TestDefaultAllowlistIsInsideTheScannedTree(t *testing.T) {
	listed := "00000000-0000-4000-8000-000000000002"
	dir := tree(t, map[string]string{
		"a.txt":                      listed + "\n",
		"testdata/pii-allowlist.txt": listed + "\n",
	})
	if r := scan(t, dir); r.code != 0 {
		t.Errorf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	dir = tree(t, map[string]string{"a.txt": listed + "\n"})
	if r := scan(t, dir); r.code != 1 {
		t.Errorf("without an allowlist the id must be flagged; exit %d", r.code)
	}
}

func TestScansPrintableStringsOfBinaries(t *testing.T) {
	bad := "\x00\x01\x02\xff\xfe" + "junk\x00" + webmail() + "\x00\x03\x04"
	dir := tree(t, map[string]string{"libexec/tool": bad})
	r := scan(t, "--allowlist", allowlist(t), dir)
	if r.code != 1 || !strings.Contains(r.stdout+r.stderr, filepath.Join("libexec", "tool")) {
		t.Errorf("binary email not found: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	dir = tree(t, map[string]string{"libexec/tool": "\x00\x01\x02\xff\xfe" + "just text" + "\x00"})
	if r := scan(t, "--allowlist", allowlist(t), dir); r.code != 0 {
		t.Errorf("clean binary flagged: %s%s", r.stdout, r.stderr)
	}
}

func TestTrackedModeScansOnlyGitTrackedFiles(t *testing.T) {
	dir := tree(t, map[string]string{"clean.txt": "ok\n", "untracked.txt": webmail() + "\n"})
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, o)
		}
	}
	run("init", "-q")
	run("add", "clean.txt")
	if r := scan(t, "--tracked", "--allowlist", allowlist(t), dir); r.code != 0 {
		t.Fatalf("untracked offender flagged: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	run("add", "untracked.txt")
	if r := scan(t, "--tracked", "--allowlist", allowlist(t), dir); r.code != 1 {
		t.Errorf("tracked offender missed: exit %d", r.code)
	}
}

func TestExcludeSkipsDirectories(t *testing.T) {
	dir := tree(t, map[string]string{"build/x.txt": webmail(), "src/y.txt": "fine"})
	if r := scan(t, "--allowlist", allowlist(t), "--exclude", "build", dir); r.code != 0 {
		t.Errorf("excluded dir scanned: %s%s", r.stdout, r.stderr)
	}
	if r := scan(t, "--allowlist", allowlist(t), dir); r.code != 1 {
		t.Errorf("without --exclude: exit %d", r.code)
	}
}

func TestUsageErrors(t *testing.T) {
	if r := scan(t); r.code != 2 {
		t.Errorf("no dir: exit %d", r.code)
	}
	if r := scan(t, filepath.Join(t.TempDir(), "missing")); r.code != 2 {
		t.Errorf("missing dir: exit %d", r.code)
	}
}

// The repository itself must stay clean. Inside the Docker image the
// git metadata of a worktree is not mounted, so fall back to walking the tree.
func TestRepositoryTreeIsClean(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "testdata", "pii-allowlist.txt")); err != nil {
		t.Fatalf("allowlist missing: %v", err)
	}
	args := []string{"--tracked", root}
	if err := exec.Command("git", "-C", root, "ls-files", "-z").Run(); err != nil {
		args = []string{"--exclude", "build", "--exclude", "dist", root}
	}
	if r := scan(t, args...); r.code != 0 {
		t.Errorf("repository is not clean (exit %d):\n%s%s", r.code, r.stdout, r.stderr)
	}
}
