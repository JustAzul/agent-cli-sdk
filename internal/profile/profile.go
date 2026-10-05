// Package profile holds the built-in scenario profiles and resolves the
// effective model, effort and sandbox for a run.
package profile

// Source values say where an effective setting came from.
const (
	SourceFlag            = "flag"
	SourceProfile         = "profile"
	SourceProviderDefault = "provider-default"
)

// Flags are the explicit values the caller passed; empty means not passed.
type Flags struct {
	Model   string
	Effort  string
	Sandbox string
}

// Resolved is the effective setting for each field and where it came from.
// An empty value means the provider's own default applies.
type Resolved struct {
	Model         string
	ModelSource   string
	Effort        string
	EffortSource  string
	Sandbox       string
	SandboxSource string
}

// setting is one scenario's defaults; empty fields mean no default.
type setting struct{ model, effort, sandbox string }

// builtin is the shipped table, keyed by provider then scenario. A
// scenario or provider that is absent has no profile.
var builtin = map[string]map[string]setting{
	"codex": {
		"second-opinion": {"gpt-6.1-sol", "high", "read-only"},
		"code-review":    {"gpt-6.1-sol", "high", "read-only"},
		"cross-check":    {"gpt-6.1-sol", "high", "read-only"},
		"expert-persona": {"gpt-6.1-sol", "xhigh", "read-only"},
		"delegation":     {"gpt-6.1-sol", "medium", "workspace-write"},
	},
}

// Resolve applies the precedence explicit flag, then profile, then provider
// default, for the provider's profile of the scenario (if it has one).
func Resolve(providerName, scenario string, f Flags) Resolved {
	p := builtin[providerName][scenario]
	var r Resolved
	r.Model, r.ModelSource = pick(f.Model, p.model)
	r.Effort, r.EffortSource = pick(f.Effort, p.effort)
	r.Sandbox, r.SandboxSource = pick(f.Sandbox, p.sandbox)
	return r
}

func pick(flagValue, profileValue string) (value, source string) {
	switch {
	case flagValue != "":
		return flagValue, SourceFlag
	case profileValue != "":
		return profileValue, SourceProfile
	}
	return "", SourceProviderDefault
}
