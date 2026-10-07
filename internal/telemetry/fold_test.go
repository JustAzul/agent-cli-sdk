package telemetry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JustAzul/agentcli/internal/telemetry"
)

func writeMonth(t *testing.T, home, month string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, "telemetry")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n")
	if len(lines) > 0 {
		body += "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, month+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runLine(id, ts, attrs string) string {
	return `{"v":1,"kind":"run","run_id":"` + id + `","ts":"` + ts + `","provider":"codex","attrs":` + attrs + `}`
}

func annLine(id, ts, attrs string) string {
	return `{"v":1,"kind":"annotation","run_id":"` + id + `","ts":"` + ts + `","attrs":` + attrs + `}`
}

func first(t *testing.T, f telemetry.Folded) map[string]any {
	t.Helper()
	if len(f.Runs) == 0 {
		t.Fatal("no runs folded")
	}
	return f.Runs[0]
}

func attrsOf(t *testing.T, run map[string]any) map[string]any {
	t.Helper()
	a, ok := run["attrs"].(map[string]any)
	if !ok {
		t.Fatalf("attrs = %#v", run["attrs"])
	}
	return a
}

func TestFoldMergesAnnotationsInAppendOrder(t *testing.T) {
	home := t.TempDir()
	writeMonth(t, home, "2026-09", runLine("r1", "2026-09-30T23:59:50Z", `{"a":"1","keep":true}`))
	writeMonth(t, home, "2026-10",
		annLine("r1", "2026-10-01T00:00:10Z", `{"a":"2","b":{"x":1}}`),
		annLine("r1", "2026-10-01T00:00:11Z", `{"b":{"y":2}}`))
	f, err := telemetry.Fold(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Runs) != 1 {
		t.Fatalf("runs = %v", f.Runs)
	}
	want := map[string]any{"a": "2", "keep": true, "b": map[string]any{"y": json.Number("2")}}
	if got := attrsOf(t, first(t, f)); !reflect.DeepEqual(got, want) {
		t.Errorf("attrs = %#v, want %#v", got, want)
	}
}

func TestFoldNullIsAValue(t *testing.T) {
	home := t.TempDir()
	writeMonth(t, home, "2026-10", runLine("r1", "2026-10-01T00:00:00Z", `{"k":1}`), annLine("r1", "2026-10-01T00:00:05Z", `{"k":null}`))
	f, _ := telemetry.Fold(home, nil)
	v, present := attrsOf(t, first(t, f))["k"]
	if !present || v != nil {
		t.Errorf("k = %#v present=%v, want present null", v, present)
	}
}

func TestFoldReplayedAnnotationIsIdempotent(t *testing.T) {
	home := t.TempDir()
	ann := annLine("r1", "2026-10-01T00:00:05Z", `{"acted":"yes"}`)
	writeMonth(t, home, "2026-10", runLine("r1", "2026-10-01T00:00:00Z", `{}`), ann, ann)
	f, _ := telemetry.Fold(home, nil)
	if got := attrsOf(t, first(t, f)); !reflect.DeepEqual(got, map[string]any{"acted": "yes"}) {
		t.Errorf("attrs = %v", got)
	}
}

func TestFoldAnnotationWithoutRunCreatesNothing(t *testing.T) {
	home := t.TempDir()
	writeMonth(t, home, "2026-10", annLine("ghost", "2026-10-01T00:00:05Z", `{"a":1}`))
	f, _ := telemetry.Fold(home, nil)
	if len(f.Runs) != 0 || f.Skipped.Total() != 0 {
		t.Errorf("runs=%v skipped=%+v", f.Runs, f.Skipped)
	}
}

func TestFoldAnnotationAppendedBeforeItsRunStillApplies(t *testing.T) {
	home := t.TempDir()
	writeMonth(t, home, "2026-10", annLine("r1", "2026-10-01T00:00:01Z", `{"early":"y"}`), runLine("r1", "2026-10-01T00:00:00Z", `{"d":"1"}`))
	f, _ := telemetry.Fold(home, nil)
	if got := attrsOf(t, first(t, f)); !reflect.DeepEqual(got, map[string]any{"early": "y", "d": "1"}) {
		t.Errorf("attrs = %v", got)
	}
}

func TestFoldSkipsAndCounts(t *testing.T) {
	home := t.TempDir()
	writeMonth(t, home, "2026-10",
		runLine("r1", "2026-10-01T00:00:00Z", `{}`),
		`{"v":2,"kind":"run","run_id":"r2","ts":"2026-10-01T00:00:00Z"}`,
		`{"v":1,"kind":"note"}`,
		`garbage`,
		`[1,2]`,
		`null`,
		``,
		`{"v":1,"kind":"run","ts":"2026-10-01T00:00:00Z"}`)
	f, _ := telemetry.Fold(home, nil)
	want := telemetry.Skipped{UnknownKind: 1, UnknownVersion: 1, Unparseable: 4}
	if f.Skipped != want || len(f.Runs) != 1 {
		t.Errorf("skipped = %+v runs=%d, want %+v and 1 run", f.Skipped, len(f.Runs), want)
	}
}

func TestFoldCountsATornLine(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "telemetry")
	os.MkdirAll(dir, 0o700)
	body := `{"v":1,"kind":"run","run_` + "\n" + runLine("r1", "2026-10-01T00:00:00Z", `{}`) + "\n"
	os.WriteFile(filepath.Join(dir, "2026-10.jsonl"), []byte(body), 0o600)
	f, _ := telemetry.Fold(home, nil)
	if f.Skipped.Unparseable != 1 || len(f.Runs) != 1 {
		t.Errorf("skipped = %+v runs=%d", f.Skipped, len(f.Runs))
	}
}

func TestFoldWindowReadsFromTheStartMonthAndFiltersByRunTS(t *testing.T) {
	home := t.TempDir()
	writeMonth(t, home, "2026-08", runLine("old", "2026-08-20T00:00:00Z", `{}`), annLine("old", "2026-08-21T00:00:00Z", `{"x":1}`))
	writeMonth(t, home, "2026-09",
		runLine("before", "2026-09-10T00:00:00Z", `{}`),
		runLine("edge", "2026-09-30T23:59:50Z", `{"a":"1"}`))
	writeMonth(t, home, "2026-10",
		annLine("edge", "2026-10-01T00:00:10Z", `{"a":"2"}`),
		annLine("old", "2026-10-01T00:00:11Z", `{"y":1}`),
		runLine("new", "2026-10-02T00:00:00Z", `{}`))
	since := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	f, err := telemetry.Fold(home, &since)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range f.Runs {
		ids = append(ids, r["run_id"].(string))
	}
	if !reflect.DeepEqual(ids, []string{"edge", "new"}) {
		t.Fatalf("ids = %v", ids)
	}
	if attrsOf(t, first(t, f))["a"] != "2" {
		t.Errorf("edge attrs = %v (annotation from the next month not folded)", first(t, f)["attrs"])
	}
	if f.Skipped.Total() != 0 {
		t.Errorf("skipped = %+v", f.Skipped)
	}
}

func TestFoldIgnoresFilesThatAreNotMonthFilesAndMissingDir(t *testing.T) {
	home := t.TempDir()
	if f, err := telemetry.Fold(home, nil); err != nil || len(f.Runs) != 0 {
		t.Fatalf("missing dir: %v %v", f, err)
	}
	writeMonth(t, home, "notes", runLine("r1", "2026-10-01T00:00:00Z", `{}`))
	if f, _ := telemetry.Fold(home, nil); len(f.Runs) != 0 {
		t.Errorf("read a non-month file: %v", f.Runs)
	}
}

func TestFoldWithNonObjectAttrsStartsEmpty(t *testing.T) {
	home := t.TempDir()
	writeMonth(t, home, "2026-10",
		`{"v":1,"kind":"run","run_id":"r1","ts":"2026-10-01T00:00:00Z","attrs":null}`,
		annLine("r1", "2026-10-01T00:00:05Z", `{"a":1}`))
	f, _ := telemetry.Fold(home, nil)
	if len(f.Runs) != 1 || len(attrsOf(t, first(t, f))) != 1 {
		t.Errorf("runs = %v", f.Runs)
	}
}
