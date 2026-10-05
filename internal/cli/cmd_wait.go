package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/JustAzul/agent-cli-sdk/internal/store"
)

func init() {
	register(Command{Name: "wait", Summary: "block until a run finishes, then print it like a foreground run", Run: runWait})
}

const waitPoll = 25 * time.Millisecond

func runWait(ctx *Context, args []string) int {
	var asJSON bool
	var timeoutS int
	fset := flag.NewFlagSet("wait", flag.ContinueOnError)
	fset.SetOutput(io.Discard)
	fset.BoolVar(&asJSON, "json", false, "print one JSON object")
	fset.Var(timeoutFlag{&timeoutS}, "timeout", "give up after this many seconds, leaving the run as it is")
	ctx.JSON = wantsJSON(args)
	positional, err := parseInterspersed(fset, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(ctx.Stderr, "usage: agentcli wait <run_id> [--timeout <seconds>] [--json]")
		return ExitOK
	}
	if err != nil {
		return ctx.Fail(ExitUsage, "%v", err)
	}
	ctx.JSON = asJSON
	if len(positional) != 1 {
		return ctx.Fail(ExitUsage, "wait takes exactly one run id")
	}

	home, err := store.ResolveHome(ctx.Getenv)
	if err != nil {
		return ctx.Fail(ExitInternal, "%v", err)
	}
	st := store.Open(home)
	state, code := loadRun(ctx, st, positional[0])
	if code != 0 {
		return code
	}
	state, code = awaitTerminal(ctx, st, state, time.Duration(timeoutS)*time.Second)
	if code != 0 {
		return code
	}

	exit := ExitInternal
	if state.ExitCode != nil {
		exit = *state.ExitCode
	}
	if asJSON {
		if printJSON(ctx, resultOf(state)) != 0 {
			return ExitInternal
		}
		return exit
	}
	fmt.Fprintln(ctx.Stdout, state.OutputPath)
	return exit
}

// awaitTerminal polls the run until it is terminal. A zero timeout waits
// without limit; when a limit passes first the run is left as it is and the
// exit is 5.
func awaitTerminal(ctx *Context, st *store.Store, state store.State, timeout time.Duration) (store.State, int) {
	var expired <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		expired = t.C
	}
	tick := time.NewTicker(waitPoll)
	defer tick.Stop()
	for !isTerminalState(state.State) {
		select {
		case <-expired:
			return state, ctx.Fail(ExitWaitTimeout, "run %s is still %s after %s", state.RunID, state.State, timeout)
		case <-tick.C:
		}
		next, err := observeRun(st, state.RunID)
		if err != nil {
			return state, ctx.Fail(ExitInternal, "reading run %s: %v", state.RunID, err)
		}
		state = next
	}
	return state, 0
}
