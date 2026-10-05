package cli

import (
	"errors"
	"os"
	"time"

	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/runner"
	"github.com/JustAzul/agent-cli-sdk/internal/store"
)

// workerCommand is the internal entry point a job's detached worker runs.
const workerCommand = "_worker"

func init() {
	register(Command{Name: workerCommand, Hidden: true, Run: runWorker})
}

// runWorker executes an admitted background run: it rebuilds the job from the
// run directory and hands it to the same runner a foreground run uses. The
// run's outcome lives in its state file; the worker's own exit is 0 unless it
// could not run the job at all.
func runWorker(ctx *Context, args []string) int {
	if len(args) != 1 || store.ValidateRunID(args[0]) != nil {
		return ctx.Fail(ExitUsage, "usage: agentcli %s <run_id>", workerCommand)
	}
	runID := args[0]
	home, err := store.ResolveHome(ctx.Getenv)
	if err != nil {
		return ctx.Fail(ExitInternal, "%v", err)
	}
	st := store.Open(home)
	state, err := st.ReadState(runID)
	if errors.Is(err, os.ErrNotExist) {
		return ctx.Fail(ExitNotFound, "no run %q", runID)
	}
	if err != nil {
		return ctx.Fail(ExitInternal, "reading the run's state: %v", err)
	}
	var req requestRecord
	if err := st.ReadRequest(runID, &req); err != nil {
		return ctx.Fail(ExitInternal, "reading the run's request: %v", err)
	}
	prompt, err := st.ReadPrompt(runID)
	if err != nil {
		return ctx.Fail(ExitInternal, "reading the run's prompt: %v", err)
	}
	prov, ok := provider.Get(req.Provider)
	if !ok {
		return ctx.Fail(ExitInternal, "unknown provider %q", req.Provider)
	}

	if d, ok := envMillis(ctx.Getenv, "AGENTCLI_TEST_WORKER_STALL_MS"); ok {
		time.Sleep(d) // lets a test hold the run between queued and running
	}
	if state.State != "queued" {
		return ExitOK // the run was settled before this worker got to it
	}
	if _, err := runner.Run(buildJob(ctx, st, prov, req, state, prompt)); err != nil {
		return ctx.Fail(ExitInternal, "%v", err)
	}
	return ExitOK
}
