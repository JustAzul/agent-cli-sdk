package cli

import (
	"errors"
	"flag"
	"fmt"
)

// turnFlags are the flags shared by commands that run a provider turn.
type turnFlags struct {
	provider      string
	scenario      string
	model         string
	effort        string
	sandbox       string
	cwd           string
	source        string
	sessionID     string
	runID         string
	promptFile    string
	cleanSentinel string
	materialLabel string
	json          bool
	dryRun        bool
}

// parsedTurn is the result of parsing a turn command's arguments.
type parsedTurn struct {
	turnFlags
	positional  []string
	passthrough []string
}

// parseTurnArgs parses the flags of a turn command with the standard library.
// Flags and positionals may be interspersed; everything after the first bare
// "--" is native passthrough, verbatim. exit is meaningful when done is true.
func parseTurnArgs(ctx *Context, name string, args []string) (p parsedTurn, exit int, done bool) {
	head := args
	for i, a := range args {
		if a == "--" {
			head = args[:i]
			p.passthrough = append([]string(nil), args[i+1:]...)
			break
		}
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	fs.Usage = func() { fmt.Fprintf(ctx.Stderr, "usage: agentcli %s [flags] <prompt | -> [-- native flags]\n", name) }
	f := &p.turnFlags
	fs.StringVar(&f.provider, "provider", "codex", "provider to run")
	fs.StringVar(&f.scenario, "scenario", "adhoc", "caller's label for the run's purpose")
	fs.StringVar(&f.model, "model", "", "model")
	fs.StringVar(&f.effort, "effort", "", "reasoning effort")
	fs.StringVar(&f.sandbox, "sandbox", "", "read-only or workspace-write")
	fs.StringVar(&f.cwd, "cwd", "", "working directory (default: current)")
	fs.StringVar(&f.source, "source", "cli", "who dispatched the run")
	fs.StringVar(&f.sessionID, "session-id", ctx.Getenv("CLAUDE_CODE_SESSION_ID"), "Claude Code session id")
	fs.StringVar(&f.runID, "run-id", "", "caller-supplied run id")
	fs.StringVar(&f.promptFile, "prompt-file", "", "read the prompt from a file")
	fs.StringVar(&f.cleanSentinel, "clean-sentinel", "", "output text that classifies the run as clean")
	fs.StringVar(&f.materialLabel, "material-label", "ok", "outcome label for material output")
	fs.BoolVar(&f.json, "json", false, "print one JSON object instead of the output path")
	fs.BoolVar(&f.dryRun, "dry-run", false, "print the provider plan and execute nothing")

	rest := head
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return p, ExitOK, true
			}
			return p, ExitUsage, true
		}
		if fs.NArg() == 0 {
			return p, 0, false
		}
		p.positional = append(p.positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
}
