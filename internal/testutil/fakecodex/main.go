// Command fakecodex is the fake provider used by tests. Its behaviour is
// driven by FAKECODEX_* environment variables; see docs/FRAME.md.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__sleeper" {
		// Grandchild started by FAKECODEX_SPAWN_CHILD: sleeps, then goes away.
		time.Sleep(60 * time.Second)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(os.Getenv("FAKECODEX_VERSION"))
		return
	}
	os.Exit(run())
}

func run() int {
	// Drain stdin first so the caller never blocks writing the prompt.
	stdin, _ := io.ReadAll(os.Stdin)

	if path := os.Getenv("FAKECODEX_RECORD"); path != "" {
		record(path, stdin)
	}
	if os.Getenv("FAKECODEX_IGNORE_TERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
	}
	if os.Getenv("FAKECODEX_SPAWN_CHILD") == "1" {
		spawnChild()
	}

	var fixture []byte
	if f := os.Getenv("FAKECODEX_FIXTURE"); f != "" {
		var err error
		if fixture, err = os.ReadFile(f); err != nil {
			fmt.Fprintf(os.Stderr, "fakecodex: %v\n", err)
			return 99
		}
		os.Stdout.Write(fixture)
	}

	if out := outputArg(os.Args[1:]); out != "" {
		text, ok := os.LookupEnv("FAKECODEX_OUTPUT")
		if !ok {
			text, ok = lastAgentMessage(fixture)
		}
		if ok {
			if err := os.WriteFile(out, []byte(text), 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "fakecodex: %v\n", err)
				return 99
			}
		}
	}

	if s := os.Getenv("FAKECODEX_STDERR"); s != "" {
		fmt.Fprint(os.Stderr, s)
	}
	if ms, _ := strconv.Atoi(os.Getenv("FAKECODEX_SLEEP_MS")); ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	code, _ := strconv.Atoi(os.Getenv("FAKECODEX_EXIT"))
	return code
}

func record(path string, stdin []byte) {
	cwd, _ := os.Getwd()
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	data, _ := json.Marshal(map[string]any{
		"argv": os.Args, "stdin": string(stdin), "env": env, "cwd": cwd,
	})
	_ = os.WriteFile(path, data, 0o600)
}

// spawnChild starts a sleeping grandchild in this process's group, detached
// from stdout/stderr so it never holds the caller's pipes open.
func spawnChild() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "__sleeper")
	_ = cmd.Start()
}

func outputArg(args []string) string {
	for i, a := range args {
		if (a == "-o" || a == "--output-last-message") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// lastAgentMessage returns the text of the fixture's last agent_message item.
func lastAgentMessage(fixture []byte) (string, bool) {
	var text string
	found := false
	sc := bufio.NewScanner(bytes.NewReader(fixture))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var ev struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Type == "item.completed" && ev.Item.Type == "agent_message" {
			text, found = ev.Item.Text, true
		}
	}
	return text, found
}
