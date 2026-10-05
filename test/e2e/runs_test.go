package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// writeMonthFile writes raw telemetry lines into home/telemetry/<month>.jsonl.
func writeMonthFile(t *testing.T, home, month string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(telemetryDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(telemetryDir(home), month+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func tsAgo(d time.Duration) string { return time.Now().UTC().Add(-d).Format(time.RFC3339) }

func monthOf(ts string) string { return ts[:7] }

// recLine builds a minimal run record line; fields is spliced in verbatim.
func recLine(id, ts, fields string) string {
	if fields != "" {
		fields = "," + fields
	}
	return fmt.Sprintf(`{"v":1,"kind":"run","run_id":%q,"ts":%q,"provider":"codex"%s}`, id, ts, fields)
}

func annotationLine(id, ts, attrs string) string {
	return fmt.Sprintf(`{"v":1,"kind":"annotation","run_id":%q,"ts":%q,"attrs":%s}`, id, ts, attrs)
}

func jsonLines(t *testing.T, r result) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range r.lines() {
		var m map[string]any
		if err := jsonUnmarshal(l, &m); err != nil {
			t.Fatalf("stdout line is not a JSON object: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func runIDs(runs []map[string]any) []string {
	var ids []string
	for _, r := range runs {
		ids = append(ids, r["run_id"].(string))
	}
	return ids
}

func TestRunsShowsAnnotationsFoldedIntoDispatchAttrs(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "rn1", "--attr", "ticket=2056")
	if r := s.run("annotate", "rn1", "--attr", "acted=yes"); r.code != 0 {
		t.Fatalf("annotate exit %d: %s", r.code, r.stderr)
	}
	r := s.run("runs", "--json")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	runs := jsonLines(t, r)
	if len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
	if want := map[string]any{"ticket": "2056", "acted": "yes"}; !reflect.DeepEqual(runs[0]["attrs"], want) {
		t.Errorf("attrs = %v, want %v", runs[0]["attrs"], want)
	}
	if runs[0]["kind"] != "run" || runs[0]["run_id"] != "rn1" {
		t.Errorf("run = %v", runs[0])
	}
}

func TestRunsNullAnnotationIsAValueAndReplayIsIdempotent(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "rn2")
	for _, a := range [][]string{{"--attr", "k=1"}, {"--attr-json", "k=null"}, {"--attr-json", "k=null"}, {"--attr", "z=1"}, {"--attr", "z=1"}} {
		if r := s.run(append([]string{"annotate", "rn2"}, a...)...); r.code != 0 {
			t.Fatalf("annotate exit %d: %s", r.code, r.stderr)
		}
	}
	runs := jsonLines(t, s.run("runs"))
	if len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
	attrs := runs[0]["attrs"].(map[string]any)
	if v, present := attrs["k"]; !present || v != nil {
		t.Errorf("attrs.k = %#v present=%v, want present null", v, present)
	}
	if !reflect.DeepEqual(attrs, map[string]any{"k": nil, "z": "1"}) {
		t.Errorf("attrs = %v", attrs)
	}
}

func TestRunsKeepsUnknownFieldsAndBigNumbers(t *testing.T) {
	s := newSandbox(t)
	ts := tsAgo(time.Hour)
	writeMonthFile(t, s.home, monthOf(ts), recLine("fu1", ts, `"future_field":{"a":[1,2]},"output_bytes":12345678901234567890,"attrs":{"n":1.50}`))
	r := s.run("runs")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	for _, want := range []string{`"future_field":{"a":[1,2]}`, `"output_bytes":12345678901234567890`, `"n":1.50`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %s: %s", want, r.stdout)
		}
	}
}

func TestRunsSkippedCountsOnOneStderrLine(t *testing.T) {
	s := newSandbox(t)
	ts := tsAgo(time.Hour)
	writeMonthFile(t, s.home, monthOf(ts),
		recLine("sk1", ts, ""),
		`{"v":2,"kind":"run","run_id":"sk2"}`,
		`{"v":1,"kind":"note"}`,
		`this is not json`)
	r := s.run("runs")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if ids := runIDs(jsonLines(t, r)); !reflect.DeepEqual(ids, []string{"sk1"}) {
		t.Errorf("runs = %v", ids)
	}
	lines := strings.Split(strings.TrimSpace(r.stderr), "\n")
	if len(lines) != 1 {
		t.Fatalf("stderr = %q, want one line", r.stderr)
	}
	for _, want := range []string{"unknown_kind=1", "unknown_version=1", "unparseable=1"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("stderr lacks %s: %q", want, lines[0])
		}
	}
	clean := newSandbox(t)
	writeMonthFile(t, clean.home, monthOf(ts), recLine("ok1", ts, ""))
	if r := clean.run("runs"); r.stderr != "" {
		t.Errorf("stderr = %q, want empty when nothing was skipped", r.stderr)
	}
}

func TestRunsWindow(t *testing.T) {
	s := newSandbox(t)
	young, old := tsAgo(3*24*time.Hour), tsAgo(10*24*time.Hour)
	// Several month files, so the 10-day record may sit in the previous month.
	byMonth := map[string][]string{}
	byMonth[monthOf(old)] = append(byMonth[monthOf(old)], recLine("old", old, ""))
	byMonth[monthOf(young)] = append(byMonth[monthOf(young)], recLine("young", young, ""))
	for m, lines := range byMonth {
		writeMonthFile(t, s.home, m, lines...)
	}
	cases := []struct {
		args []string
		want []string
		code int
	}{
		{[]string{"runs"}, []string{"young"}, 0},
		{[]string{"runs", "--days", "30"}, []string{"old", "young"}, 0},
		{[]string{"runs", "--all"}, []string{"old", "young"}, 0},
		{[]string{"runs", "--days", "2"}, nil, 0},
		{[]string{"runs", "--days", "3", "--all"}, nil, 2},
		{[]string{"runs", "--days", "-1"}, nil, 2},
		{[]string{"runs", "--days", "x"}, nil, 2},
		{[]string{"runs", "extra"}, nil, 2},
	}
	for _, c := range cases {
		r := s.run(c.args...)
		if r.code != c.code {
			t.Errorf("%v: exit %d, want %d (stderr %q)", c.args, r.code, c.code, r.stderr)
			continue
		}
		if c.code != 0 {
			continue
		}
		got := runIDs(jsonLines(t, r))
		sort.Strings(got)
		if len(got) != len(c.want) || (len(got) > 0 && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("%v: runs = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestRunsMonthBoundaryFoldsAcrossBothMonthFiles(t *testing.T) {
	s := newSandbox(t)
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	edge := first.Add(-10 * time.Second).Format(time.RFC3339)    // last seconds of the previous month
	early := first.Add(-5 * 24 * time.Hour).Format(time.RFC3339) // earlier in the previous month
	writeMonthFile(t, s.home, early[:7], recLine("early", early, `"attrs":{"a":"1"}`))
	writeMonthFile(t, s.home, first.Format("2006-01"),
		recLine("edge", edge, `"attrs":{"d":"1"}`),
		annotationLine("edge", first.Add(time.Second).Format(time.RFC3339), `{"acted":"yes"}`),
		annotationLine("early", first.Add(2*time.Second).Format(time.RFC3339), `{"a":"2"}`))
	r := s.run("runs", "--days", "40")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	byID := map[string]map[string]any{}
	for _, run := range jsonLines(t, r) {
		byID[run["run_id"].(string)] = run["attrs"].(map[string]any)
	}
	if !reflect.DeepEqual(byID["edge"], map[string]any{"d": "1", "acted": "yes"}) ||
		!reflect.DeepEqual(byID["early"], map[string]any{"a": "2"}) || len(byID) != 2 {
		t.Errorf("folded = %v", byID)
	}
}

func TestRunsIgnoresAnnotationsForRunsOutsideTheWindow(t *testing.T) {
	s := newSandbox(t)
	ts := tsAgo(time.Hour)
	writeMonthFile(t, s.home, monthOf(ts), recLine("in", ts, ""), annotationLine("gone", ts, `{"a":1}`))
	r := s.run("runs", "--all")
	if ids := runIDs(jsonLines(t, r)); !reflect.DeepEqual(ids, []string{"in"}) || r.stderr != "" {
		t.Errorf("runs = %v stderr %q", ids, r.stderr)
	}
}

func TestRunsOnAnEmptyHomeIsEmpty(t *testing.T) {
	s := newSandbox(t)
	if r := s.run("runs"); r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestRunsRecordsSurviveRoundTripThroughJSON(t *testing.T) {
	s := newSandbox(t)
	seedRun(t, s, "rt1")
	r := s.run("runs")
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.stdout)), &m); err != nil || m["run_id"] != "rt1" {
		t.Fatalf("stdout = %q (%v)", r.stdout, err)
	}
	want := oneRecord(t, s.home)
	if !reflect.DeepEqual(m, want) {
		t.Errorf("runs output differs from the stored record\n got %v\nwant %v", m, want)
	}
}
