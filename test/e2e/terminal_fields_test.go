package e2e

import (
	"testing"
	"time"
)

// terminalFieldCase is one way a run can end and the provider_exit and
// sdk_status its state file records for it.
type terminalFieldCase struct {
	name         string
	state        string
	providerExit any
	sdkStatus    string
	setup        func(*sandbox) *sandbox
	args         []string
}

func terminalFieldCases() []terminalFieldCase {
	slow := func(s *sandbox) *sandbox {
		return shutdownEnv(s, 500*time.Millisecond, 500*time.Millisecond).set("FAKECODEX_SLEEP_MS", "60000")
	}
	return []terminalFieldCase{
		{name: "done", state: "done", providerExit: float64(0), sdkStatus: "ok",
			setup: func(s *sandbox) *sandbox { return s.set("FAKECODEX_FIXTURE", fixture("exec-ok")) }},
		{name: "provider failure", state: "failed", providerExit: float64(3), sdkStatus: "ok",
			setup: func(s *sandbox) *sandbox { return s.set("FAKECODEX_EXIT", "3") }},
		{name: "timeout", state: "timeout", providerExit: float64(143), sdkStatus: "timeout", setup: slow,
			args: []string{"--timeout", "1"}},
		{name: "provider missing", state: "failed", providerExit: nil, sdkStatus: "provider_missing",
			setup: func(s *sandbox) *sandbox { return s.withoutProvider() }},
	}
}

// Every terminal transition records the provider's exit and the SDK status in
// state.json, and the readers print what was recorded.
func TestTerminalStateRecordsProviderExitAndSDKStatus(t *testing.T) {
	for _, tc := range terminalFieldCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.setup(newSandbox(t))
			args := append([]string{"exec", "--json", "--run-id", "tf1"}, tc.args...)
			fg := s.run(append(args, "q")...)
			want := map[string]any{"state": tc.state, "provider_exit": tc.providerExit, "sdk_status": tc.sdkStatus}
			checkFields(t, "foreground json", fg.json(t), want)
			checkFields(t, "state.json", stateOf(t, s, "tf1"), want)
			checkFields(t, "status json", s.run("status", "--json", "tf1").json(t), want)
			checkFields(t, "wait json", s.run("wait", "--json", "tf1").json(t), want)
		})
	}
}

// A state file that records these fields is printed as recorded, even where
// deriving them from the state would say otherwise.
func TestReadersPrintTheRecordedTerminalFields(t *testing.T) {
	s := newSandbox(t)
	seedRunFiles(t, s, "tf2", map[string]any{
		"state": "cancelled", "exit_code": 130, "outcome": "cancelled",
		"provider_exit": 143, "sdk_status": "cancelled",
	}, nil, nil)
	want := map[string]any{"provider_exit": float64(143), "sdk_status": "cancelled"}
	checkFields(t, "status json", s.run("status", "--json", "tf2").json(t), want)
	checkFields(t, "wait json", s.run("wait", "--json", "tf2").json(t), want)
}

// A run directory written before these fields existed is derived on read.
func TestReadersDeriveTerminalFieldsWhenTheStateLacksThem(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		want  map[string]any
	}{
		{"done", map[string]any{"state": "done", "exit_code": 0},
			map[string]any{"provider_exit": float64(0), "sdk_status": "ok"}},
		{"cancelled", map[string]any{"state": "cancelled", "exit_code": 130},
			map[string]any{"provider_exit": nil, "sdk_status": "cancelled"}},
		{"provider missing", map[string]any{"state": "failed", "exit_code": 127},
			map[string]any{"provider_exit": nil, "sdk_status": "provider_missing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSandbox(t)
			seedRunFiles(t, s, "tf3", tc.state, nil, nil)
			checkFields(t, "status json", s.run("status", "--json", "tf3").json(t), tc.want)
			checkFields(t, "wait json", s.run("wait", "--json", "tf3").json(t), tc.want)
		})
	}
}
