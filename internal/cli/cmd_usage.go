package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/JustAzul/agentcli/internal/provider"
	"github.com/JustAzul/agentcli/internal/store"
	"github.com/JustAzul/agentcli/internal/telemetry"
)

func init() {
	register(Command{Name: "usage", Summary: "usage add records the token usage of a model call", Run: runUsage})
}

const usageAddLine = "usage: agentcli usage add --session-id <id> --provider anthropic --model <model> --source <source> [--run-id <run_id>] [--json]"

// modelCallProviders maps each provider a model call can be recorded for to
// the price-list namespace (litellm_provider) its models are priced under.
// These providers have no model catalog.
var modelCallProviders = map[string]string{"anthropic": "anthropic"}

func runUsage(ctx *Context, args []string) int {
	if len(args) > 0 && args[0] == "add" {
		return runUsageAdd(ctx, args[1:])
	}
	ctx.JSON = wantsJSON(args)
	fmt.Fprintln(ctx.Stderr, usageAddLine)
	return ctx.Fail(ExitUsage, "usage needs a subcommand: add")
}

// usageAddFlags is what `usage add` was asked to record.
type usageAddFlags struct {
	sessionID, provider, model, source, runID string
	runIDGiven, asJSON                        bool
}

// parseUsageAddFlags reads the flags of `usage add` and the arguments left
// after them.
func parseUsageAddFlags(args []string) (usageAddFlags, []string, error) {
	var f usageAddFlags
	fset := flag.NewFlagSet("usage add", flag.ContinueOnError)
	fset.SetOutput(io.Discard)
	fset.StringVar(&f.sessionID, "session-id", "", "the Claude Code session id")
	fset.StringVar(&f.provider, "provider", "", "the model provider")
	fset.StringVar(&f.model, "model", "", "the model called")
	fset.StringVar(&f.source, "source", "", "what made the call")
	fset.StringVar(&f.runID, "run-id", "", "the run the call was about")
	fset.BoolVar(&f.asJSON, "json", false, "print one JSON object")
	positional, err := parseInterspersed(fset, args)
	fset.Visit(func(given *flag.Flag) { f.runIDGiven = f.runIDGiven || given.Name == "run-id" })
	return f, positional, err
}

// problem names the first thing `usage add` cannot take, or "" when there is
// none.
func (f usageAddFlags) problem(positional []string) string {
	switch {
	case len(positional) != 0:
		return fmt.Sprintf("usage add takes no arguments, got %q", positional[0])
	case f.sessionID == "":
		return "usage add needs a non-empty --session-id"
	case f.model == "":
		return "usage add needs a non-empty --model"
	case f.source == "":
		return "usage add needs a non-empty --source"
	}
	if _, ok := modelCallProviders[f.provider]; !ok {
		return fmt.Sprintf("--provider must be anthropic, got %q", f.provider)
	}
	return ""
}

// modelCall is the record of one call with usage, recorded at now. An absent
// --run-id records null.
func (f usageAddFlags) modelCall(usage provider.Usage, now time.Time) telemetry.ModelCall {
	call := telemetry.ModelCall{
		CallID: store.NewCallID(now), TS: now.Format(time.RFC3339), SessionID: f.sessionID,
		Provider: f.provider, Model: f.model, Source: f.source, Usage: usage,
	}
	if f.runIDGiven {
		call.RunID = &f.runID
	}
	return call
}

func runUsageAdd(ctx *Context, args []string) int {
	ctx.JSON = wantsJSON(args)
	f, positional, err := parseUsageAddFlags(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(ctx.Stderr, usageAddLine)
		return ExitOK
	}
	if err != nil {
		return ctx.Fail(ExitUsage, "%v", err)
	}
	ctx.JSON = f.asJSON
	if problem := f.problem(positional); problem != "" {
		return ctx.Fail(ExitUsage, "%s", problem)
	}

	raw, err := io.ReadAll(ctx.Stdin)
	if err != nil {
		return ctx.Fail(ExitInternal, "reading stdin: %v", err)
	}
	usage, err := NormalizeAnthropicUsage(raw)
	if err != nil {
		return ctx.Fail(ExitUsage, "stdin: %v", err)
	}
	report := printUsageAddText
	if f.asJSON {
		report = printUsageAddJSON
	}
	if usage.IsZero() {
		return report(ctx, "")
	}

	home, err := store.ResolveHome(ctx.Getenv)
	if err != nil {
		return ctx.Fail(ExitInternal, "%v", err)
	}
	now := ctx.Now().UTC().Truncate(time.Second)
	call := f.modelCall(usage, now)
	if err := telemetry.AppendModelCall(home, call, now, telemetry.Options{}); err != nil {
		return ctx.Fail(ExitInternal, "writing the model call: %v", err)
	}
	return report(ctx, call.CallID)
}

// printUsageAddJSON reports the outcome as one JSON object; an empty callID
// means nothing was recorded.
func printUsageAddJSON(ctx *Context, callID string) int {
	var id any
	if callID != "" {
		id = callID
	}
	out, err := json.Marshal(struct {
		SDKStatus string `json:"sdk_status"`
		ExitCode  int    `json:"exit_code"`
		Recorded  bool   `json:"recorded"`
		CallID    any    `json:"call_id"`
	}{"ok", ExitOK, callID != "", id})
	if err != nil {
		return ctx.Fail(ExitInternal, "%v", err)
	}
	fmt.Fprintf(ctx.Stdout, "%s\n", out)
	return ExitOK
}

// printUsageAddText prints the call id, or nothing when nothing was recorded.
func printUsageAddText(ctx *Context, callID string) int {
	if callID != "" {
		fmt.Fprintln(ctx.Stdout, callID)
	}
	return ExitOK
}

// anthropicUsage is the usage object Anthropic reports. A missing key reads
// as zero; any other key is ignored.
type anthropicUsage struct {
	Input         nonNegInt `json:"input_tokens"`
	Output        nonNegInt `json:"output_tokens"`
	CacheRead     nonNegInt `json:"cache_read_input_tokens"`
	CacheCreation nonNegInt `json:"cache_creation_input_tokens"`
}

// nonNegInt decodes a JSON non-negative integer and nothing else.
type nonNegInt int64

func (n *nonNegInt) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return fmt.Errorf("%s is not a non-negative integer", b)
	}
	var num json.Number
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&num); err != nil {
		return fmt.Errorf("%s is not a non-negative integer", b)
	}
	v, err := num.Int64()
	if err != nil || v < 0 {
		return fmt.Errorf("%s is not a non-negative integer", b)
	}
	*n = nonNegInt(v)
	return nil
}

// NormalizeAnthropicUsage turns Anthropic's usage object (one JSON object)
// into agentcli's usage shape: input is the total input including the cache
// read and cache creation tokens, cached is the cache reads, cache write is
// the cache creations, and reasoning is zero. It fails when the input is not
// one JSON object, or a count is not a non-negative integer, or the input
// total overflows.
func NormalizeAnthropicUsage(raw []byte) (provider.Usage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return provider.Usage{}, errors.New("expected one JSON object")
	}
	var in anthropicUsage
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if err := dec.Decode(&in); err != nil {
		return provider.Usage{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return provider.Usage{}, errors.New("expected one JSON object")
	}
	input, read, write := int64(in.Input), int64(in.CacheRead), int64(in.CacheCreation)
	if input > math.MaxInt64-read || input+read > math.MaxInt64-write {
		return provider.Usage{}, errors.New("token counts are too large")
	}
	return provider.Usage{
		InputTokens:           input + read + write,
		CachedInputTokens:     read,
		CacheWriteInputTokens: write,
		OutputTokens:          int64(in.Output),
	}, nil
}
