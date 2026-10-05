package cli

import (
	"os"
	"time"

	"github.com/JustAzul/agent-cli-sdk/internal/profile"
	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/store"
)

type admission struct {
	command        string // exec, review or send
	runID          string
	conversationID string
	existing       *store.Conversation // nil: this turn starts a new conversation
	flags          turnFlags
	effective      profile.Resolved // profile and flags merged
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

// admit reserves the turn on its conversation (creating the conversation for
// a first turn), then creates the run directory and writes prompt.md,
// request.json and the queued state. A conversation with a turn still queued
// or running yields a *store.BusyError and nothing is written. If the run
// cannot be created, the reservation is released.
func admit(st *store.Store, a admission) (store.State, error) {
	turn, err := reserve(st, a)
	if err != nil {
		return store.State{}, err
	}
	state, err := writeRun(st, a, turn)
	if err != nil {
		_ = st.Release(a.conversationID, a.runID)
		return store.State{}, err
	}
	return state, nil
}

func reserve(st *store.Store, a admission) (int, error) {
	if a.existing != nil {
		return st.Reserve(a.conversationID, a.runID, a.now)
	}
	f, e := a.flags, a.effective
	ts := a.now.Format(time.RFC3339)
	err := st.ReserveNew(store.Conversation{
		ConversationID: a.conversationID, Provider: f.provider, Cwd: a.cwd,
		Defaults:  store.Defaults{Scenario: f.scenario, Model: e.Model, Effort: e.Effort, Sandbox: e.Sandbox},
		CreatedAt: ts, UpdatedAt: ts,
	}, a.runID, a.now)
	return 1, err
}

func writeRun(st *store.Store, a admission, turn int) (store.State, error) {
	if err := st.CreateRunDir(a.runID); err != nil {
		return store.State{}, err
	}
	if err := st.WritePrompt(a.runID, a.prompt); err != nil {
		return store.State{}, err
	}
	f, e := a.flags, a.effective
	passthrough := a.passthrough
	if passthrough == nil {
		passthrough = []string{}
	}
	req := requestRecord{
		Command: a.command, Provider: f.provider, Scenario: f.scenario,
		Model: nullable(e.Model), ModelSource: e.ModelSource,
		Effort: nullable(e.Effort), EffortSource: e.EffortSource,
		Sandbox: nullable(e.Sandbox), SandboxSource: e.SandboxSource,
		Cwd: a.cwd, Source: f.source, SessionID: f.sessionID,
		CleanSentinel: f.cleanSentinel, MaterialLabel: f.materialLabel,
		Attrs: f.attrs.object(), Passthrough: passthrough, Plan: a.plan,
	}
	if err := st.WriteRequest(a.runID, req); err != nil {
		return store.State{}, err
	}
	state := store.State{
		RunID: a.runID, ConversationID: a.conversationID, Turn: turn, State: "queued",
		AdmittedAt: a.now.Format(time.RFC3339), WorkerPID: os.Getpid(),
		OutputPath: st.OutputPath(a.runID), RunDir: st.RunDir(a.runID),
	}
	if err := st.WriteState(a.runID, state); err != nil {
		return store.State{}, err
	}
	return state, nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
