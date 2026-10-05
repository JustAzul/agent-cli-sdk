package cli

import (
	"errors"
	"os"

	"github.com/JustAzul/agent-cli-sdk/internal/profile"
	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/store"
)

func init() {
	register(Command{Name: "send", Summary: "run the next turn of a conversation", Run: runSend})
}

var sendSpec = turnSpec{name: "send", usage: "[flags] <conversation_id | run_id> <prompt | -> [-- native flags]"}

func runSend(ctx *Context, args []string) int {
	p, exit, done := parseTurnArgs(ctx, sendSpec, args)
	if done {
		return exit
	}
	if len(p.positional) == 0 {
		return ctx.Fail(ExitUsage, "send needs the conversation id or a run id of the conversation")
	}
	target := p.positional[0]
	p.positional = p.positional[1:]

	home, err := store.ResolveHome(ctx.Getenv)
	if err != nil {
		return ctx.Fail(ExitInternal, "%v", err)
	}
	st := store.Open(home)
	conv, err := findConversation(st, target)
	if errors.Is(err, os.ErrNotExist) {
		return ctx.Fail(ExitNotFound, "no conversation or run %q", target)
	}
	if err != nil {
		return ctx.Fail(ExitInternal, "reading the conversation: %v", err)
	}
	ctx.IDs["conversation_id"] = conv.ConversationID

	if p.given["provider"] && p.provider != conv.Provider {
		return ctx.Fail(ExitUsage, "conversation %s uses provider %q, not %q", conv.ConversationID, conv.Provider, p.provider)
	}
	p.provider = conv.Provider
	prov, ok := provider.Get(p.provider)
	if !ok {
		return ctx.Fail(ExitUsage, "unknown provider %q (known: %v)", p.provider, provider.Names())
	}

	if !p.given["scenario"] {
		p.scenario = conv.Defaults.Scenario
	}
	eff := turnSettings(st, conv, p)
	if err := prov.Capabilities().Unsupported(provider.Needs{Command: "resume", Sandbox: eff.Sandbox}); err != nil {
		return ctx.Fail(ExitUsage, "provider %q %v", p.provider, err)
	}
	if flagText, reserved := reservedPassthrough(prov, p.passthrough); reserved {
		return ctx.Fail(ExitUsage, "native flag %q is reserved by agentcli and cannot be passed after --", flagText)
	}

	if len(p.positional)+boolToInt(p.promptFile != "") != 1 {
		return ctx.Fail(ExitUsage, "exactly one prompt source is required: a positional prompt, - for stdin, or --prompt-file")
	}
	prompt, code := readPrompt(ctx, p)
	if code != 0 {
		return code
	}

	cwd, code := turnCwd(ctx, conv, p)
	if code != 0 {
		return code
	}

	// Re-read so the checks below see the conversation as it is now.
	if conv, err = st.ReadConversation(conv.ConversationID); err != nil {
		return ctx.Fail(ExitInternal, "reading the conversation: %v", err)
	}
	if conv.ActiveRunID != nil {
		return ctx.Fail(ExitBusy, "conversation %s is busy: run %s is still queued or running", conv.ConversationID, *conv.ActiveRunID)
	}
	if conv.ProviderSessionID == nil {
		return ctx.Fail(ExitNotResumable, "conversation %s is not resumable: the provider never reported a provider session id for it", conv.ConversationID)
	}

	return runTurn(ctx, turn{
		command: "send", flags: p, prov: prov, eff: eff, cwd: cwd, prompt: prompt, st: st, existing: &conv,
		req: provider.Request{
			Command: "resume", Cwd: cwd, Model: eff.Model, Effort: eff.Effort, Sandbox: eff.Sandbox,
			SessionID: *conv.ProviderSessionID, Passthrough: p.passthrough,
		},
	})
}

// findConversation resolves an id that names a conversation or one of its
// runs. The error wraps os.ErrNotExist when neither exists.
func findConversation(st *store.Store, id string) (store.Conversation, error) {
	if store.ValidateRunID(id) != nil {
		return store.Conversation{}, os.ErrNotExist
	}
	conv, err := st.ReadConversation(id)
	if !errors.Is(err, os.ErrNotExist) {
		return conv, err
	}
	state, err := st.ReadState(id)
	if err != nil {
		return store.Conversation{}, err
	}
	return st.ReadConversation(state.ConversationID)
}

// turnSettings merges this turn's explicit flags over the conversation's
// stored defaults. A stored value keeps the source its first turn recorded.
func turnSettings(st *store.Store, conv store.Conversation, p parsedTurn) profile.Resolved {
	first := firstTurnSources(st, conv)
	pick := func(name, flagValue, stored, storedSource string) (string, string) {
		switch {
		case p.given[name] && flagValue != "":
			return flagValue, profile.SourceFlag
		case stored == "":
			return "", profile.SourceProviderDefault
		}
		return stored, storedSource
	}
	var r profile.Resolved
	r.Model, r.ModelSource = pick("model", p.model, conv.Defaults.Model, first.model)
	r.Effort, r.EffortSource = pick("effort", p.effort, conv.Defaults.Effort, first.effort)
	r.Sandbox, r.SandboxSource = pick("sandbox", p.sandbox, conv.Defaults.Sandbox, first.sandbox)
	return r
}

type settingSources struct{ model, effort, sandbox string }

// firstTurnSources reads where the first turn got its model, effort and
// sandbox from; a missing or unreadable record counts as the profile.
func firstTurnSources(st *store.Store, conv store.Conversation) settingSources {
	out := settingSources{profile.SourceProfile, profile.SourceProfile, profile.SourceProfile}
	if len(conv.Turns) == 0 {
		return out
	}
	var req struct {
		ModelSource   string `json:"model_source"`
		EffortSource  string `json:"effort_source"`
		SandboxSource string `json:"sandbox_source"`
	}
	if st.ReadRequest(conv.Turns[0], &req) != nil {
		return out
	}
	for dst, src := range map[*string]string{&out.model: req.ModelSource, &out.effort: req.EffortSource, &out.sandbox: req.SandboxSource} {
		if src != "" {
			*dst = src
		}
	}
	return out
}

// turnCwd is --cwd when given, else the conversation's recorded directory,
// which must still exist.
func turnCwd(ctx *Context, conv store.Conversation, p parsedTurn) (string, int) {
	if p.given["cwd"] {
		return resolveCwd(ctx, p.cwd, p.dryRun)
	}
	if info, err := os.Stat(conv.Cwd); err != nil || !info.IsDir() {
		return "", ctx.Fail(ExitUsage, "the conversation's working directory %q no longer exists; pass --cwd to run this turn elsewhere", conv.Cwd)
	}
	return conv.Cwd, 0
}
