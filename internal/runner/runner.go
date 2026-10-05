// Package runner turns a provider plan into a finished run: spawn, stream
// events, classify the outcome and write the terminal state.
package runner

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/JustAzul/agent-cli-sdk/internal/provider"
	"github.com/JustAzul/agent-cli-sdk/internal/store"
	"github.com/JustAzul/agent-cli-sdk/internal/telemetry"
)

const stderrTailBytes = 64 * 1024

// Job is everything the runner needs; the caller has already admitted the run.
type Job struct {
	Store    *store.Store
	Provider provider.Provider
	Plan     provider.Plan
	Prompt   []byte
	Env      []string    // caller environment, inherited by the provider
	State    store.State // the admitted (queued) state
	// CleanSentinel and MaterialLabel drive outcome classification.
	CleanSentinel string
	MaterialLabel string
	Warn          func(string)
	Now           func() time.Time

	// Record carries the telemetry fields known before the run starts; the
	// runner fills in the rest and appends it when the run is terminal.
	Record telemetry.Record

	startedAt time.Time
	version   *string
}

// Result describes a finished run.
type Result struct {
	State        store.State
	ProviderExit *int // nil when the provider never ran to an exit
	ExitCode     int  // the process exit code agentcli should use
	SDKStatus    string
	Usage        *provider.Usage
}

// SDK status values reported alongside a result.
const (
	SDKStatusOK              = "ok"
	SDKStatusProviderMissing = "provider_missing"
)

// Run executes a foreground run to a terminal state. It returns an error only
// for internal failures that prevent the run from starting.
func Run(job Job) (Result, error) {
	j := &job
	if j.Warn == nil {
		j.Warn = func(string) {}
	}
	if j.Now == nil {
		j.Now = time.Now
	}

	st := j.State
	j.startedAt = j.Now()
	started := j.startedAt.UTC().Format(time.RFC3339)
	st.State = "running"
	st.StartedAt = &started
	st.WorkerPID = os.Getpid()
	if err := j.Store.WriteState(st.RunID, st); err != nil {
		return Result{}, fmt.Errorf("writing the running state: %w", err)
	}

	bin := j.Plan.Argv[0]
	path, err := lookPath(bin, j.Env)
	if err != nil {
		excerpt := fmt.Sprintf("provider binary %q not found on PATH", bin)
		return j.finish(st, outcome{state: "failed", exitCode: 127, label: "error", excerpt: excerpt, sdkStatus: SDKStatusProviderMissing}), nil
	}

	cmd := exec.Command(path, j.Plan.Argv[1:]...)
	cmd.Dir = j.Plan.Dir
	cmd.Env = buildEnv(j.Env, j.Plan.EnvAdd, j.Plan.EnvRemove)
	env := cmd.Env
	switch j.Plan.Stdin {
	case provider.StdinPrompt:
		cmd.Stdin = bytes.NewReader(j.Prompt)
	default:
		cmd.Stdin = bytes.NewReader(nil)
	}
	tail := newTailBuffer(stderrTailBytes)
	cmd.Stderr = tail
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("opening the provider stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		excerpt := fmt.Sprintf("starting provider %q: %v", bin, err)
		return j.finish(st, outcome{state: "failed", exitCode: 127, label: "error", excerpt: excerpt, sdkStatus: SDKStatusProviderMissing}), nil
	}

	// The version probe runs beside the provider so it adds no wall time.
	versionCh := make(chan *string, 1)
	go func() { versionCh <- providerVersion(path, j.Provider.VersionArgs(), j.Plan.Dir, env) }()

	stream := j.consume(stdout, st)
	waitErr := cmd.Wait()
	j.version = <-versionCh
	exit, err := exitCode(waitErr)
	if err != nil {
		return Result{}, fmt.Errorf("waiting for the provider: %w", err)
	}

	if terr := j.Store.WriteStderrTail(st.RunID, tail.Bytes()); terr != nil {
		j.Warn(fmt.Sprintf("could not write stderr.tail: %v", terr))
	}
	st.UnparsedEvents = stream.unparsed
	return j.finish(st, outcome{
		exitCode: exit, providerExit: &exit, sdkStatus: SDKStatusOK, usage: stream.usage, sessionID: stream.sessionID,
		eventError: stream.lastError, stderrLine: lastNonEmptyLine(tail.Bytes()),
	}), nil
}

// appendTelemetry writes the run's record, the last step of finish. A
// failure only warns.
func (j *Job) appendTelemetry(st store.State, label, excerpt string, o outcome) {
	rec := j.Record
	rec.V, rec.Kind = telemetry.Version, "run"
	rec.RunID, rec.ConversationID, rec.Turn = st.RunID, st.ConversationID, st.Turn
	rec.TS = j.startedAt.UTC().Format(time.RFC3339)
	rec.ProviderVersion = j.version
	rec.ExitCode, rec.Outcome = o.exitCode, label
	rec.DurationMS = j.Now().Sub(j.startedAt).Milliseconds()
	rec.OutputFile = st.OutputPath
	if info, err := os.Stat(st.OutputPath); err == nil {
		rec.OutputBytes = info.Size()
	}
	rec.ErrorExcerpt = nullable(excerpt)
	rec.Usage = provider.NormalizeUsage(rec.Command, o.usage)
	rec.ProviderSessionID = nullable(o.sessionID)
	if err := telemetry.Append(j.Store.Home, rec, j.Now(), telemetry.Options{}); err != nil {
		j.Warn(fmt.Sprintf("could not append the telemetry record: %v", err))
	}
}

func (j *Job) stamp() string { return j.Now().UTC().Format(time.RFC3339) }

type streamResult struct {
	unparsed  int
	lastError string
	usage     *provider.Usage
	sessionID string
}

// consume parses provider stdout line by line as it arrives and persists the
// provider session id as soon as it is seen.
func (j *Job) consume(r io.Reader, st store.State) streamResult {
	var res streamResult
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			ev, ok := j.Provider.ParseEvent(trimmed)
			switch {
			case !ok:
				res.unparsed++
			default:
				if ev.SessionID != "" && ev.SessionID != res.sessionID {
					res.sessionID = ev.SessionID
					j.persistSession(st.ConversationID, res.sessionID)
				}
				if ev.Usage != nil {
					res.usage = ev.Usage
				}
				if ev.ErrorMsg != "" {
					res.lastError = ev.ErrorMsg
				}
			}
		}
		if err != nil {
			return res
		}
	}
}

func (j *Job) persistSession(conversationID, sessionID string) {
	err := j.Store.UpdateConversation(conversationID, func(c *store.Conversation) {
		c.ProviderSessionID = &sessionID
		c.UpdatedAt = j.stamp()
	})
	if err != nil {
		j.Warn(fmt.Sprintf("could not persist the provider session id: %v", err))
	}
}

type outcome struct {
	state        string // forced state; empty means derive from the exit code
	exitCode     int
	providerExit *int
	label        string // forced outcome; empty means classify the output
	excerpt      string // forced error excerpt
	sdkStatus    string
	usage        *provider.Usage
	eventError   string
	stderrLine   string
	sessionID    string
}

// finish classifies the run and writes the terminal state in this order:
// output final, state terminal, conversation marker cleared.
func (j *Job) finish(st store.State, o outcome) Result {
	_ = os.Chmod(st.OutputPath, 0o600) // the provider wrote it; keep it user-only

	label := o.label
	if label == "" {
		label = classify(o.exitCode, st.OutputPath, j.CleanSentinel, j.MaterialLabel)
	}
	excerpt := o.excerpt
	if excerpt == "" {
		excerpt = errorExcerpt(o.eventError, o.exitCode, o.stderrLine)
	}
	state := o.state
	if state == "" {
		state = "done"
		if o.exitCode != 0 {
			state = "failed"
		}
	}

	ended := j.stamp()
	exit := o.exitCode
	st.State = state
	st.EndedAt = &ended
	st.ExitCode = &exit
	st.Outcome = &label
	st.ErrorExcerpt = nullable(excerpt)
	if err := j.Store.WriteState(st.RunID, st); err != nil {
		j.Warn(fmt.Sprintf("could not write the terminal state: %v", err))
	}
	err := j.Store.UpdateConversation(st.ConversationID, func(c *store.Conversation) {
		c.ActiveRunID = nil
		c.UpdatedAt = ended
	})
	if err != nil {
		j.Warn(fmt.Sprintf("could not clear the conversation's active run: %v", err))
	}
	j.appendTelemetry(st, label, excerpt, o)
	return Result{State: st, ProviderExit: o.providerExit, ExitCode: exit, SDKStatus: o.sdkStatus, Usage: o.usage}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// classify assigns the outcome of a run that ended on its own.
func classify(exit int, outputPath, sentinel, materialLabel string) string {
	if exit != 0 {
		return "error"
	}
	data, err := os.ReadFile(outputPath)
	if err != nil || len(data) == 0 {
		return "empty"
	}
	if sentinel != "" && normalize(string(data)) == normalize(sentinel) {
		return "clean"
	}
	return materialLabel
}

func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// exitCode maps a Wait error to a process exit code: the provider's own exit,
// or 128+n when a signal ended it.
func exitCode(waitErr error) (int, error) {
	if waitErr == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if !errors.As(waitErr, &ee) {
		return 0, waitErr
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return ee.ExitCode(), nil
}

func lastNonEmptyLine(b []byte) string {
	lines := strings.Split(strings.ReplaceAll(string(b), "\r", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}
