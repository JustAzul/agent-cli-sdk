package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/runner"
	"github.com/JustAzul/agent-cli-sdk/internal/store"
)

func init() {
	register(Command{Name: "exec", Summary: "start a conversation and run its first turn", Run: runExec})
}

func runExec(ctx *Context, args []string) int {
	p, exit, done := parseTurnArgs(ctx, "exec", args)
	if done {
		return exit
	}

	prov, ok := provider.Get(p.provider)
	if !ok {
		ctx.Errorf("unknown provider %q (known: %v)", p.provider, provider.Names())
		return ExitUsage
	}
	caps := prov.Capabilities()
	if !caps.Supports("exec") {
		ctx.Errorf("provider %q does not support the exec command", p.provider)
		return ExitUsage
	}
	if p.sandbox != "" && !caps.SupportsSandbox(p.sandbox) {
		ctx.Errorf("unsupported --sandbox %q for provider %q (supported: %v)", p.sandbox, p.provider, caps.Sandboxes)
		return ExitUsage
	}

	if len(p.positional)+boolToInt(p.promptFile != "") != 1 {
		ctx.Errorf("exactly one prompt source is required: a positional prompt, - for stdin, or --prompt-file")
		return ExitUsage
	}
	prompt, code := readPrompt(ctx, p)
	if code != 0 {
		return code
	}

	cwd, code := resolveCwd(ctx, p.cwd, p.dryRun)
	if code != 0 {
		return code
	}

	home, err := store.ResolveHome(ctx.Getenv)
	if err != nil {
		ctx.Errorf("%v", err)
		return ExitInternal
	}
	st := store.Open(home)
	now := ctx.Now().UTC()
	runID, code := chooseRunID(ctx, st, p.runID, now)
	if code != 0 {
		return code
	}

	outputPath := st.OutputPath(runID)
	req := provider.Request{
		Command:     "exec",
		Cwd:         cwd,
		Model:       p.model,
		Effort:      p.effort,
		Sandbox:     p.sandbox,
		Passthrough: p.passthrough,
		OutputPath:  outputPath,
	}
	plan, err := prov.BuildPlan(req)
	if err != nil {
		ctx.Errorf("building the provider plan: %v", err)
		return ExitInternal
	}

	if p.dryRun {
		return printJSON(ctx, struct {
			Command  string `json:"command"`
			Provider string `json:"provider"`
			RunID    string `json:"run_id"`
			provider.Plan
		}{"exec", p.provider, runID, plan})
	}

	conversationID := store.NewConversationID(now)
	if err := admit(st, admission{
		runID: runID, conversationID: conversationID, flags: p.turnFlags,
		cwd: cwd, prompt: prompt, passthrough: p.passthrough, plan: plan, now: now,
	}); err != nil {
		if errors.Is(err, store.ErrRunExists) {
			ctx.Errorf("run id %q already exists", runID)
			return ExitUsage
		}
		ctx.Errorf("admitting the run: %v", err)
		return ExitInternal
	}

	state, err := st.ReadState(runID)
	if err != nil {
		ctx.Errorf("reading the admitted state: %v", err)
		return ExitInternal
	}
	res, err := runner.Run(runner.Job{
		Store: st, Provider: prov, Plan: plan, Prompt: prompt, Env: ctx.Env, State: state,
		CleanSentinel: p.cleanSentinel, MaterialLabel: p.materialLabel,
		Warn: func(msg string) { ctx.Warnf("%s", msg) }, Now: ctx.Now,
	})
	if err != nil {
		ctx.Errorf("%v", err)
		return ExitInternal
	}

	if p.json {
		return printRunResult(ctx, res)
	}
	io.WriteString(ctx.Stdout, res.State.OutputPath+"\n")
	return res.ExitCode
}

// printRunResult prints the FR5 --json object for a finished run and returns its
// exit code.
func printRunResult(ctx *Context, res runner.Result) int {
	s := res.State
	out := struct {
		ConversationID string  `json:"conversation_id"`
		RunID          string  `json:"run_id"`
		State          string  `json:"state"`
		Outcome        *string `json:"outcome"`
		SDKStatus      string  `json:"sdk_status"`
		ProviderExit   *int    `json:"provider_exit"`
		ExitCode       int     `json:"exit_code"`
		OutputPath     string  `json:"output_path"`
		RunDir         string  `json:"run_dir"`
	}{s.ConversationID, s.RunID, s.State, s.Outcome, res.SDKStatus, res.ProviderExit, res.ExitCode, s.OutputPath, s.RunDir}
	if printJSON(ctx, out) != 0 {
		return ExitInternal
	}
	return res.ExitCode
}

func printJSON(ctx *Context, v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		ctx.Errorf("encoding output: %v", err)
		return ExitInternal
	}
	ctx.Stdout.Write(append(data, '\n'))
	return 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// readPrompt returns the prompt for the single chosen source. Dry runs do not
// consume standard input.
func readPrompt(ctx *Context, p parsedTurn) ([]byte, int) {
	switch {
	case p.promptFile != "":
		data, err := os.ReadFile(p.promptFile)
		if err != nil {
			ctx.Errorf("reading --prompt-file: %v", err)
			return nil, ExitUsage
		}
		return data, 0
	case p.positional[0] == "-":
		if p.dryRun {
			return nil, 0
		}
		data, err := io.ReadAll(ctx.Stdin)
		if err != nil {
			ctx.Errorf("reading the prompt from standard input: %v", err)
			return nil, ExitUsage
		}
		return data, 0
	default:
		return []byte(p.positional[0]), 0
	}
}

// resolveCwd makes the working directory absolute. A real run requires it to
// exist; a dry run only describes the plan.
func resolveCwd(ctx *Context, flagValue string, dryRun bool) (string, int) {
	cwd := flagValue
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			ctx.Errorf("cannot determine the working directory: %v", err)
			return "", ExitInternal
		}
		cwd = wd
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		ctx.Errorf("--cwd: %v", err)
		return "", ExitUsage
	}
	if !dryRun {
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			ctx.Errorf("--cwd %q is not an existing directory", abs)
			return "", ExitUsage
		}
	}
	return abs, 0
}

// chooseRunID validates a caller-supplied run id or generates a fresh one.
func chooseRunID(ctx *Context, st *store.Store, supplied string, now time.Time) (string, int) {
	if supplied != "" {
		if err := store.ValidateRunID(supplied); err != nil {
			ctx.Errorf("invalid --run-id: %v", err)
			return "", ExitUsage
		}
		if st.RunExists(supplied) {
			ctx.Errorf("run id %q already exists", supplied)
			return "", ExitUsage
		}
		return supplied, 0
	}
	for i := 0; i < 5; i++ {
		id := store.NewRunID(now)
		if !st.RunExists(id) {
			return id, 0
		}
	}
	ctx.Errorf("could not generate a unique run id")
	return "", ExitInternal
}
