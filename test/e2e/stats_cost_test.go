package e2e

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// The three runs the cost cases price together: 0.68 + 0.26 + 0.012 = 0.952.
func threeModelRuns() []string {
	return []string{
		usageRun("m-sol", tokens{input: 1000000, cached: 800000, output: 10000}),
		usageRun("m-old", tokens{input: 100000, cached: 60000, output: 1000}),
		usageRun("m-bare", tokens{input: 10000, cached: 4000, output: 500}),
	}
}

func costStats(t *testing.T, s *sandbox, args ...string) map[string]any {
	t.Helper()
	r := s.run(append([]string{"stats", "--all", "--json"}, args...)...)
	if r.code != 0 {
		t.Fatalf("stats exit %d: %s", r.code, r.stderr)
	}
	return r.json(t)
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%#v is not a JSON object", v)
	}
	return m
}

func TestStatsPricesARunFromTheCache(t *testing.T) {
	cases := []struct {
		name, model, run, want string
	}{
		{"estimated cache writes", "m-sol", usageRun("m-sol", tokens{input: 1000000, cached: 800000, output: 10000}), "0.680000"},
		{"reported cache writes win", "m-sol", usageRun("m-sol", tokens{input: 1000000, cached: 800000, write: 50000, output: 10000}), "0.605000"},
		{"reasoning is not counted again", "m-sol", usageRun("m-sol", tokens{input: 1000000, cached: 800000, output: 10000, reasoning: 5000}), "0.680000"},
		{"omitted cache prices", "m-old", usageRun("m-old", tokens{input: 100000, cached: 60000, output: 1000}), "0.260000"},
		{"no cache fields at all", "m-bare", usageRun("m-bare", tokens{input: 10000, cached: 4000, output: 500}), "0.012000"},
		{"half to even down", "m-half", usageRun("m-half", tokens{input: 1}), "0.000000"},
		{"half to even up", "m-half", usageRun("m-half", tokens{input: 3}), "0.000002"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t)
			costCache(t, s)
			seedModelRuns(t, s, c.run)

			stats := costStats(t, s)

			byModel := obj(t, obj(t, stats["usage_by_model"])[c.model])
			if byModel["cost_usd"] != c.want {
				t.Errorf("usage_by_model.%s.cost_usd = %v, want %s", c.model, byModel["cost_usd"], c.want)
			}
			totals := obj(t, stats["usage_totals"])
			if totals["cost_usd"] != c.want || totals["cost_complete"] != true {
				t.Errorf("usage_totals cost_usd = %v cost_complete = %v, want %s and true", totals["cost_usd"], totals["cost_complete"], c.want)
			}
			if _, ok := byModel["cost_complete"]; ok {
				t.Error("a usage_by_model entry carries cost_complete")
			}
		})
	}
}

func TestStatsSumsRunCostsBeforeRounding(t *testing.T) {
	s := newSandbox(t)
	costCache(t, s)
	seedModelRuns(t, s, threeModelRuns()...)

	stats := costStats(t, s)

	totals := obj(t, stats["usage_totals"])
	if totals["cost_usd"] != "0.952000" || totals["cost_complete"] != true {
		t.Errorf("usage_totals cost_usd = %v cost_complete = %v", totals["cost_usd"], totals["cost_complete"])
	}
	byModel := obj(t, stats["usage_by_model"])
	for model, want := range map[string]string{"m-sol": "0.680000", "m-old": "0.260000", "m-bare": "0.012000"} {
		if got := obj(t, byModel[model])["cost_usd"]; got != want {
			t.Errorf("usage_by_model.%s.cost_usd = %v, want %s", model, got, want)
		}
	}
	if got := stats["prices_checked_at"]; got != costCheckedAt {
		t.Errorf("prices_checked_at = %v, want %s", got, costCheckedAt)
	}
	if !reflect.DeepEqual(stats["unpriced_models"], []any{}) || !reflect.DeepEqual(stats["missing_prices"], []any{}) {
		t.Errorf("unpriced_models = %v, missing_prices = %v, want both empty", stats["unpriced_models"], stats["missing_prices"])
	}
}

func TestStatsCountsHookRunsOfASession(t *testing.T) {
	s := newSandbox(t)
	costCache(t, s)
	seedModelRuns(t, s,
		usageRun("m-sol", tokens{input: 1000000, cached: 800000, output: 10000}, `"source":"hook-stop"`, `"session_id":"S"`),
		usageRun("m-old", tokens{input: 100000, cached: 60000, output: 1000}, `"source":"hook-post-commit"`, `"session_id":"S"`),
		usageRun("m-bare", tokens{input: 10000, cached: 4000, output: 500}, `"source":"cli"`, `"session_id":"S"`),
		usageRun("m-sol", tokens{input: 1000000, cached: 800000, output: 10000}, `"source":"cli"`, `"session_id":"other"`),
	)

	stats := costStats(t, s, "--session-id", "S")

	if got := obj(t, stats["usage_totals"])["cost_usd"]; got != "0.952000" {
		t.Errorf("usage_totals.cost_usd = %v, want 0.952000", got)
	}
}

func TestStatsTextShowsTheCost(t *testing.T) {
	s := newSandbox(t)
	costCache(t, s)
	seedModelRuns(t, s, threeModelRuns()...)

	r := s.run("stats", "--all")

	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	lines := r.lines()
	var tokens, costs []string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "tokens"):
			tokens = append(tokens, l)
		case strings.HasPrefix(l, "cost"):
			costs = append(costs, l)
		}
	}
	for _, l := range tokens {
		if strings.Contains(l, "cost_usd") || strings.Contains(l, "cost_complete") {
			t.Errorf("a tokens line carries a cost key: %q", l)
		}
	}
	want := []string{"cost: $0.95", "cost m-bare: $0.01", "cost m-old: $0.26", "cost m-sol: $0.68"}
	if !reflect.DeepEqual(costs, want) {
		t.Errorf("cost lines = %q, want %q\n%s", costs, want, r.stdout)
	}
	if len(tokens) == 0 || len(tokens)+len(costs) > len(lines) || lines[len(lines)-len(costs)-1] != tokens[len(tokens)-1] {
		t.Errorf("the cost lines do not follow the tokens lines:\n%s", r.stdout)
	}
}

func TestStatsReportsUnpricedAndMissingModels(t *testing.T) {
	s := newSandbox(t)
	costCache(t, s)
	seedModelRuns(t, s,
		usageRun("m-sol", tokens{input: 1000000, cached: 800000, output: 10000}),
		usageRun("m-hidden", tokens{input: 1000, output: 10}),
		usageRun("m-new", tokens{input: 1000, output: 10}),
		usageRun("", tokens{input: 1000, output: 10}),
	)

	stats := costStats(t, s)

	totals := obj(t, stats["usage_totals"])
	if totals["cost_complete"] != false || totals["cost_usd"] != "0.680000" {
		t.Errorf("usage_totals cost_usd = %v cost_complete = %v, want 0.680000 and false", totals["cost_usd"], totals["cost_complete"])
	}
	if got := strs(t, stats["unpriced_models"]); !reflect.DeepEqual(got, []string{"m-hidden", "m-new", "unknown"}) {
		t.Errorf("unpriced_models = %v", got)
	}
	if got := strs(t, stats["missing_prices"]); !reflect.DeepEqual(got, []string{"m-new"}) {
		t.Errorf("missing_prices = %v", got)
	}
	byModel := obj(t, stats["usage_by_model"])
	for _, model := range []string{"m-hidden", "m-new", "unknown"} {
		if got, ok := obj(t, byModel[model])["cost_usd"]; !ok || got != nil {
			t.Errorf("usage_by_model.%s.cost_usd = %v (present %v), want null", model, got, ok)
		}
	}

	text := s.run("stats", "--all").stdout
	for _, want := range []string{"cost: $0.68 (incomplete: m-hidden, m-new, unknown)\n", "cost m-sol: $0.68\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		for _, unpriced := range []string{"cost m-hidden", "cost m-new", "cost unknown"} {
			if strings.HasPrefix(line, unpriced) {
				t.Errorf("text has a cost line for an unpriced model: %q", line)
			}
		}
	}
}

func TestStatsWithoutAPriceCache(t *testing.T) {
	s := newSandbox(t)
	seedModelRuns(t, s, usageRun("m-sol", tokens{input: 1000, output: 10}), usageRun("m-hidden", tokens{input: 1000, output: 10}), usageRun("", tokens{input: 1000, output: 10}))

	stats := costStats(t, s)

	totals := obj(t, stats["usage_totals"])
	if cost, ok := totals["cost_usd"]; !ok || cost != nil || totals["cost_complete"] != false {
		t.Errorf("usage_totals cost_usd = %v (present %v) cost_complete = %v, want null and false", cost, ok, totals["cost_complete"])
	}
	if got, ok := stats["prices_checked_at"]; !ok || got != nil {
		t.Errorf("prices_checked_at = %v (present %v), want null", got, ok)
	}
	if got := strs(t, stats["missing_prices"]); !reflect.DeepEqual(got, []string{"m-hidden", "m-sol"}) {
		t.Errorf("missing_prices = %v, want every model used except unknown", got)
	}
	if got := strs(t, stats["unpriced_models"]); !reflect.DeepEqual(got, []string{"m-hidden", "m-sol", "unknown"}) {
		t.Errorf("unpriced_models = %v", got)
	}
	if text := s.run("stats", "--all").stdout; strings.Contains(text, "cost") {
		t.Errorf("text mentions a cost with nothing priced:\n%s", text)
	}
}

func TestStatsWithOnlyUnpricedRuns(t *testing.T) {
	s := newSandbox(t)
	costCache(t, s)
	seedModelRuns(t, s, usageRun("m-hidden", tokens{input: 1000, output: 10}))

	stats := costStats(t, s)

	totals := obj(t, stats["usage_totals"])
	if cost, ok := totals["cost_usd"]; !ok || cost != nil || totals["cost_complete"] != false {
		t.Errorf("usage_totals cost_usd = %v (present %v) cost_complete = %v, want null and false", cost, ok, totals["cost_complete"])
	}
	if got := stats["prices_checked_at"]; got != costCheckedAt {
		t.Errorf("prices_checked_at = %v, want %s", got, costCheckedAt)
	}
	if got := strs(t, stats["missing_prices"]); len(got) != 0 {
		t.Errorf("missing_prices = %v, want none: the cache knows m-hidden", got)
	}
}

func TestStatsCostWithoutUsage(t *testing.T) {
	s := newSandbox(t)
	costCache(t, s)
	seedModelRuns(t, s, `"model_used":"m-sol","usage":null`, `"model_used":"m-sol"`)

	stats := costStats(t, s)

	totals := obj(t, stats["usage_totals"])
	if cost, ok := totals["cost_usd"]; !ok || cost != nil || totals["cost_complete"] != true {
		t.Errorf("usage_totals cost_usd = %v (present %v) cost_complete = %v, want null and true", cost, ok, totals["cost_complete"])
	}
	if got := strs(t, stats["unpriced_models"]); len(got) != 0 {
		t.Errorf("unpriced_models = %v, want none", got)
	}
	if text := s.run("stats", "--all").stdout; strings.Contains(text, "cost") {
		t.Errorf("text mentions a cost with no usage:\n%s", text)
	}
}

func TestStatsTreatsACorruptPriceCacheAsAbsent(t *testing.T) {
	corrupt := map[string]func(t *testing.T, s *sandbox){
		"not JSON": func(t *testing.T, s *sandbox) {
			if err := os.WriteFile(pricesPath(s), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"another version": func(t *testing.T, s *sandbox) {
			c := readJSONFile(t, pricesPath(s))
			c["v"] = 2
			writeCache(t, s, c)
		},
	}
	for name, spoil := range corrupt {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			costCache(t, s)
			spoil(t, s)
			seedModelRuns(t, s, usageRun("m-sol", tokens{input: 1000000, cached: 800000, output: 10000}))

			stats := costStats(t, s)

			totals := obj(t, stats["usage_totals"])
			if cost, ok := totals["cost_usd"]; !ok || cost != nil {
				t.Errorf("usage_totals.cost_usd = %v (present %v), want null", cost, ok)
			}
			if got, ok := stats["prices_checked_at"]; !ok || got != nil {
				t.Errorf("prices_checked_at = %v (present %v), want null", got, ok)
			}
		})
	}
}
