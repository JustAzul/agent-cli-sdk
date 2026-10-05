package cli

import (
	"os"
	"time"

	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/store"
)

type admission struct {
	runID          string
	conversationID string
	flags          turnFlags
	cwd            string
	prompt         []byte
	passthrough    []string
	plan           provider.Plan
	now            time.Time
}

// requestRecord is runs/<run_id>/request.json: the resolved request and plan.
type requestRecord struct {
	Command       string         `json:"command"`
	Provider      string         `json:"provider"`
	Scenario      string         `json:"scenario"`
	Model         *string        `json:"model"`
	ModelSource   string         `json:"model_source"`
	Effort        *string        `json:"effort"`
	EffortSource  string         `json:"effort_source"`
	Sandbox       *string        `json:"sandbox"`
	SandboxSource string         `json:"sandbox_source"`
	Cwd           string         `json:"cwd"`
	Source        string         `json:"source"`
	SessionID     string         `json:"session_id"`
	TimeoutS      *int           `json:"timeout_s"`
	CleanSentinel string         `json:"clean_sentinel"`
	MaterialLabel string         `json:"material_label"`
	Attrs         map[string]any `json:"attrs"`
	Passthrough   []string       `json:"passthrough"`
	Plan          provider.Plan  `json:"plan"`
}

// admit creates the run directory, writes prompt.md, request.json and the
// queued state, and creates the conversation record with turn 1.
func admit(st *store.Store, a admission) error {
	if err := st.CreateRunDir(a.runID); err != nil {
		return err
	}
	if err := st.WritePrompt(a.runID, a.prompt); err != nil {
		return err
	}
	f := a.flags
	passthrough := a.passthrough
	if passthrough == nil {
		passthrough = []string{}
	}
	req := requestRecord{
		Command: "exec", Provider: f.provider, Scenario: f.scenario,
		Model: nullable(f.model), ModelSource: source(f.model),
		Effort: nullable(f.effort), EffortSource: source(f.effort),
		Sandbox: nullable(f.sandbox), SandboxSource: source(f.sandbox),
		Cwd: a.cwd, Source: f.source, SessionID: f.sessionID,
		CleanSentinel: f.cleanSentinel, MaterialLabel: f.materialLabel,
		Attrs: map[string]any{}, Passthrough: passthrough, Plan: a.plan,
	}
	if err := st.WriteRequest(a.runID, req); err != nil {
		return err
	}
	ts := a.now.Format(time.RFC3339)
	state := store.State{
		RunID: a.runID, ConversationID: a.conversationID, Turn: 1, State: "queued",
		AdmittedAt: ts, WorkerPID: os.Getpid(),
		OutputPath: st.OutputPath(a.runID), RunDir: st.RunDir(a.runID),
	}
	if err := st.WriteState(a.runID, state); err != nil {
		return err
	}
	return st.CreateConversation(store.Conversation{
		ConversationID: a.conversationID, Provider: f.provider, Cwd: a.cwd,
		Defaults:    store.Defaults{Scenario: f.scenario, Model: f.model, Effort: f.effort, Sandbox: f.sandbox},
		Turns:       []string{a.runID},
		ActiveRunID: &a.runID,
		CreatedAt:   ts, UpdatedAt: ts,
	})
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func source(flagValue string) string {
	if flagValue != "" {
		return "flag"
	}
	return "provider-default"
}
