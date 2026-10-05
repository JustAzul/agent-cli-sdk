package e2e

import (
	"testing"
)

// An absent value is null in the run record, never an empty string.
func TestTelemetrySessionIDNullWhenNoSessionKnown(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok"))
	if r := s.run("exec", "--run-id", "ns1", "q"); r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	rec := oneRecord(t, s.home)
	v, present := rec["session_id"]
	if !present || v != nil {
		t.Errorf("session_id = %#v (present=%v), want null", v, present)
	}
}

func TestTelemetrySessionIDNullWhenFlagEmpty(t *testing.T) {
	s := newSandbox(t).set("FAKECODEX_FIXTURE", fixture("exec-ok")).set("CLAUDE_CODE_SESSION_ID", "from-env")
	if r := s.run("exec", "--run-id", "ns2", "--session-id", "", "q"); r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	if v := oneRecord(t, s.home)["session_id"]; v != nil {
		t.Errorf("session_id = %#v, want null", v)
	}
}
