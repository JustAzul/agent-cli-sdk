// Package cli parses arguments, dispatches commands and owns the
// stdout/stderr/exit-code contract.
package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	// Provider adapters register themselves from init().
	_ "github.com/JustAzul/agent-cli-sdk/internal/provider/codex"
)

// Context is what a command sees of its process.
type Context struct {
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

// Getenv returns the last value set for key in the caller environment.
func (c *Context) Getenv(key string) string {
	v := ""
	prefix := key + "="
	for _, kv := range c.Env {
		if strings.HasPrefix(kv, prefix) {
			v = kv[len(prefix):]
		}
	}
	return v
}

// Warnf prints one warning line on stderr.
func (c *Context) Warnf(format string, a ...any) {
	fmt.Fprintf(c.Stderr, "agentcli: warning: "+format+"\n", a...)
}

// Errorf prints one error line on stderr.
func (c *Context) Errorf(format string, a ...any) {
	fmt.Fprintf(c.Stderr, "agentcli: "+format+"\n", a...)
}

// Command is one registered subcommand.
type Command struct {
	Name    string
	Summary string
	Run     func(ctx *Context, args []string) int
}

var commands = map[string]Command{}

// register adds a command; each command file calls it from init().
func register(c Command) {
	if _, dup := commands[c.Name]; dup {
		panic("command registered twice: " + c.Name)
	}
	commands[c.Name] = c
}

// Main runs agentcli with os.Args-shaped args (args[0] is the program name)
// and the caller environment, and returns the process exit code.
func Main(args []string, env []string) int {
	return Run(args, env, os.Stdin, os.Stdout, os.Stderr)
}

// Run is Main with injectable streams.
func Run(args []string, env []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx := &Context{Env: env, Stdin: stdin, Stdout: stdout, Stderr: stderr, Now: time.Now}
	if len(args) < 2 {
		usage(stderr)
		return ExitUsage
	}
	switch args[1] {
	case "-h", "--help", "help":
		usage(stdout)
		return ExitOK
	}
	cmd, ok := commands[args[1]]
	if !ok {
		ctx.Errorf("unknown command %q", args[1])
		usage(stderr)
		return ExitUsage
	}
	return cmd.Run(ctx, args[2:])
}

func usage(w io.Writer) {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "usage: agentcli <command> [flags] [prompt] [-- native flags]")
	fmt.Fprintln(w, "commands:")
	for _, n := range names {
		fmt.Fprintf(w, "  %-10s %s\n", n, commands[n].Summary)
	}
}
