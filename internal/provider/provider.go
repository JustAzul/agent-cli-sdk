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
	Command     string // "exec", "review" or "resume"
	Cwd         string
	Model       string
	Effort      string
	Sandbox     string
	SessionID   string // provider session id, for resume
	Passthrough []string
	OutputPath  string
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

// Event is what one provider output line reveals.
type Event struct {
	SessionID string // non-empty when this event reveals the provider session
	Usage     *Usage // non-nil on usage-bearing events
	ErrorMsg  string // non-empty on error-bearing events
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
