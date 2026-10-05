package cli

import (
	"errors"
	"io"
	"time"

	"github.com/JustAzul/agent-cli-sdk/internal/profile"
	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/runner"
	"github.com/JustAzul/agent-cli-sdk/internal/store"
	"github.com/JustAzul/agent-cli-sdk/internal/telemetry"
)

// turn is one fully validated provider turn, ready to plan, admit and run.
type turn struct {
	command  string // exec, review or send: the telemetry command
	flags    parsedTurn
	prov     provider.Provider
	eff      profile.Resolved
	cwd      string
	prompt   []byte
	req      provider.Request // everything but the output path
	st       *store.Store
	existing *store.Conversation // nil: the turn starts a new conversation
}

// runTurn plans the turn and, unless it is a dry run, admits and runs it in
// the foreground, printing the result per the output contract.
func runTurn(ctx *Context, t turn) int {
	p := t.flags
	now := ctx.Now().UTC()
	runID, code := chooseRunID(ctx, t.st, p.runID, now)
	if code != 0 {
		return code
	}
	t.req.OutputPath = t.st.OutputPath(runID)
	plan, err := t.prov.BuildPlan(t.req)
	if err != nil {
		return ctx.Fail(ExitInternal, "building the provider plan: %v", err)
	}

	if p.dryRun {
		return printJSON(ctx, struct {
			Command  string `json:"command"`
			Provider string `json:"provider"`
			RunID    string `json:"run_id"`
			provider.Plan
		}{t.command, p.provider, runID, plan})
	}

	conversationID := ""
	if t.existing != nil {
		conversationID = t.existing.ConversationID
	} else {
		conversationID = store.NewConversationID(now)
	}
	ctx.IDs["run_id"], ctx.IDs["conversation_id"] = runID, conversationID
	state, err := admit(t.st, admission{
		command: t.command, runID: runID, conversationID: conversationID, existing: t.existing,
		flags: p.turnFlags, effective: t.eff, cwd: t.cwd, prompt: t.prompt,
		passthrough: p.passthrough, plan: plan, now: now,
	})
	if err != nil {
		return failAdmission(ctx, err, runID, conversationID, t.existing != nil)
	}

	res, err := runner.Run(runner.Job{
		Store: t.st, Provider: t.prov, Plan: plan, Prompt: t.prompt, Env: ctx.Env, State: state,
		CleanSentinel: p.cleanSentinel, MaterialLabel: p.materialLabel,
		Timeout:  time.Duration(p.timeoutS) * time.Second,
		Shutdown: runner.ShutdownFromEnv(ctx.Getenv),
		Warn:     func(msg string) { ctx.Warnf("%s", msg) }, Now: ctx.Now,
		Record: telemetry.Record{
			Provider: p.provider, Command: t.command, Scenario: p.scenario,
			Model: nullable(t.eff.Model), ModelSource: t.eff.ModelSource,
			Effort: nullable(t.eff.Effort), EffortSource: t.eff.EffortSource,
			Sandbox: nullable(t.eff.Sandbox), Source: p.source, SessionID: nullable(p.sessionID),
			TimeoutS: timeoutSeconds(p.timeoutS), Cwd: t.cwd, Attrs: p.attrs.object(),
		},
	})
	if err != nil {
		return ctx.Fail(ExitInternal, "%v", err)
	}

	if p.json {
		return printRunResult(ctx, res)
	}
	io.WriteString(ctx.Stdout, res.State.OutputPath+"\n")
	return res.ExitCode
}

// failAdmission maps an admission error to its exit. Ids that never came to
// exist are dropped from the JSON error object.
func failAdmission(ctx *Context, err error, runID, conversationID string, existing bool) int {
	delete(ctx.IDs, "run_id")
	var busy *store.BusyError
	switch {
	case errors.As(err, &busy):
		return ctx.Fail(ExitBusy, "conversation %s is busy: run %s is still queued or running", conversationID, busy.ActiveRunID)
	case errors.Is(err, store.ErrRunExists):
		if !existing {
			delete(ctx.IDs, "conversation_id")
		}
		return ctx.Fail(ExitUsage, "run id %q already exists", runID)
	}
	return ctx.Fail(ExitInternal, "admitting the run: %v", err)
}
