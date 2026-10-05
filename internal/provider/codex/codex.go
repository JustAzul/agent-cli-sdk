// Package codex adapts the Codex CLI: plan building and event parsing.
package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JustAzul/agent-cli-sdk/internal/provider"
)

func init() { provider.Register(New()) }

type adapter struct{}

// New returns the Codex adapter.
func New() provider.Provider { return adapter{} }

func (adapter) Name() string { return "codex" }

func (adapter) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Commands:      []string{"exec", "review", "resume"},
		ReviewTargets: []string{"base", "uncommitted", "commit"},
		Sandboxes:     []string{"read-only", "workspace-write"},
		ReportsUsage:  true,
	}
}

// ReservedFlag is filled in by the reserved-flag slice.
func (adapter) ReservedFlag(arg string) bool { return false }

func (adapter) VersionArgs() []string { return []string{"--version"} }

func (adapter) BuildPlan(req provider.Request) (provider.Plan, error) {
	if req.Command != "exec" {
		return provider.Plan{}, fmt.Errorf("codex: command %q is not implemented yet", req.Command)
	}
	argv := []string{"codex", "exec", "-C", req.Cwd}
	if req.Sandbox != "" {
		argv = append(argv, "-s", req.Sandbox)
	}
	if req.Model != "" {
		argv = append(argv, "-m", req.Model)
	}
	if req.Effort != "" {
		argv = append(argv, "-c", "model_reasoning_effort="+req.Effort)
	}
	argv = append(argv, "--json", "-o", req.OutputPath)
	if !insideGitWorkTree(req.Cwd) {
		argv = append(argv, "--skip-git-repo-check")
	}
	argv = append(argv, req.Passthrough...)
	argv = append(argv, "-")
	return provider.Plan{
		Argv:      argv,
		Stdin:     provider.StdinPrompt,
		Dir:       req.Cwd,
		EnvAdd:    map[string]string{},
		EnvRemove: []string{},
	}, nil
}

// insideGitWorkTree walks up from dir looking for a .git entry (a directory,
// or a file for worktrees and submodules).
func insideGitWorkTree(dir string) bool {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

type rawEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Usage    *struct {
		InputTokens           int64 `json:"input_tokens"`
		CachedInputTokens     int64 `json:"cached_input_tokens"`
		CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
		OutputTokens          int64 `json:"output_tokens"`
		ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (adapter) ParseEvent(line []byte) (provider.Event, bool) {
	var raw rawEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return provider.Event{}, false
	}
	switch raw.Type {
	case "thread.started":
		return provider.Event{SessionID: raw.ThreadID}, true
	case "turn.completed":
		if raw.Usage == nil {
			return provider.Event{}, true
		}
		u := provider.Usage(*raw.Usage)
		return provider.Event{Usage: &u}, true
	case "error":
		return provider.Event{ErrorMsg: unwrapMessage(raw.Message)}, true
	case "turn.failed":
		if raw.Error == nil {
			return provider.Event{}, true
		}
		return provider.Event{ErrorMsg: unwrapMessage(raw.Error.Message)}, true
	}
	return provider.Event{}, true
}

// unwrapMessage returns .error.message when msg is itself a JSON document
// carrying one, else msg unchanged.
func unwrapMessage(msg string) string {
	var inner struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(msg), &inner) == nil && inner.Error != nil && inner.Error.Message != "" {
		return inner.Error.Message
	}
	return msg
}
