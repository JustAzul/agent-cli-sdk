// Package provider defines the contract between the SDK and agent CLIs.
package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Capabilities declares what a provider supports.
type Capabilities struct {
	Commands          []string `json:"commands"`
	ReviewTargets     []string `json:"review_targets"`
	Sandboxes         []string `json:"sandboxes"`
	ReportsUsage      bool     `json:"reports_usage"`
	PreassignsSession bool     `json:"preassigns_session"`
}

// Supports reports whether the provider supports the command.
func (c Capabilities) Supports(command string) bool { return contains(c.Commands, command) }

// SupportsSandbox reports whether the provider supports the sandbox mode.
func (c Capabilities) SupportsSandbox(mode string) bool { return contains(c.Sandboxes, mode) }

// Needs is what one request requires of a provider.
type Needs struct {
	Command      string // "exec", "review" or "resume"
	Sandbox      string // empty when no sandbox mode is requested
	ReviewTarget string // empty when no review target is requested
}

// Unsupported returns an error naming the first capability the request needs
// that the provider does not declare, or nil.
func (c Capabilities) Unsupported(n Needs) error {
	if n.Command != "" && !c.Supports(n.Command) {
		return fmt.Errorf("does not support the %s command (supported: %v)", n.Command, c.Commands)
	}
	if n.Sandbox != "" && !c.SupportsSandbox(n.Sandbox) {
		return fmt.Errorf("does not support sandbox mode %q (supported: %v)", n.Sandbox, c.Sandboxes)
	}
	if n.ReviewTarget != "" && !contains(c.ReviewTargets, n.ReviewTarget) {
		return fmt.Errorf("does not support review target %q (supported: %v)", n.ReviewTarget, c.ReviewTargets)
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// StdinSource says what the provider receives on standard input.
type StdinSource string

const (
	StdinPrompt StdinSource = "prompt"
	StdinEmpty  StdinSource = "empty"
)

// Request is the provider-neutral description of one turn.
type Request struct {
	Command   string // "exec", "review" or "resume"
	Cwd       string
	Model     string
	Effort    string
	Sandbox   string
	SessionID string // provider session id, for resume
	// ReviewTarget is "base", "uncommitted" or "commit" for a review; ReviewRef
	// is the branch or commit it names (empty for "uncommitted").
	ReviewTarget string
	ReviewRef    string
	Passthrough  []string
	OutputPath   string
}

// Plan is how to start the provider process.
type Plan struct {
	Argv      []string          `json:"argv"` // argv[0] is the provider binary name
	Stdin     StdinSource       `json:"stdin"`
	Dir       string            `json:"cwd"`
	EnvAdd    map[string]string `json:"env_add"`
	EnvRemove []string          `json:"env_remove"`
}

// Usage is the token accounting a provider reports.
type Usage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

// IsZero reports whether every counter is zero.
func (u Usage) IsZero() bool { return u == Usage{} }

// NormalizeUsage returns the usage to record for a command: a review
// whose usage is all zeros reports nothing, so it records null.
func NormalizeUsage(command string, u *Usage) *Usage {
	if command == "review" && u != nil && u.IsZero() {
		return nil
	}
	return u
}

// Event is what one provider output line reveals.
type Event struct {
	SessionID string    // non-empty when this event reveals the provider session
	Usage     *Usage    // non-nil on usage-bearing events
	ErrorMsg  string    // non-empty on error-bearing events
	Progress  *Progress // non-nil on events that show what the provider is doing
}

// Progress is one thing the provider did while it worked, as a person
// following the run would read it.
type Progress struct {
	Kind string // command (a command it started), message (text it wrote) or reasoning
	Text string
}

// Provider adapts one agent CLI.
type Provider interface {
	Name() string
	Capabilities() Capabilities
	ReservedFlag(arg string) (reserved bool)
	BuildPlan(req Request) (Plan, error)
	ParseEvent(line []byte) (Event, bool) // false: unparseable line
	VersionArgs() []string
}

var (
	mu       sync.RWMutex
	registry = map[string]Provider{}
)

// Register adds a provider to the registry. It panics on duplicate names.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[p.Name()]; dup {
		panic(fmt.Sprintf("provider %q registered twice", p.Name()))
	}
	registry[p.Name()] = p
}

// Get returns the named provider.
func Get(name string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	return p, ok
}

// Names lists registered providers, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
