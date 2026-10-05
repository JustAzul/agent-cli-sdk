package telemetry

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// cleanOutputMaxBytes is the output size at or below which a successful hook
// review counts as a proxy for "no findings".
const cleanOutputMaxBytes = 40

const proxyNote = "output_bytes is a PROXY for findings volume over SUCCESSFUL runs; " +
	"falls back to this when the structured findings field is absent " +
	"(records predating schema enrichment)"

// Pair is one key of an Obj.
type Pair struct {
	Key   string
	Value any
}

// Obj is a JSON object that keeps its keys in insertion order.
type Obj struct{ pairs []Pair }

// Set adds a key, or replaces its value while keeping its place.
func (o *Obj) Set(key string, value any) {
	for i := range o.pairs {
		if o.pairs[i].Key == key {
			o.pairs[i].Value = value
			return
		}
	}
	o.pairs = append(o.pairs, Pair{key, value})
}

// Get returns a key's value, or nil.
func (o *Obj) Get(key string) any {
	for _, p := range o.pairs {
		if p.Key == key {
			return p.Value
		}
	}
	return nil
}

// Pairs lists the keys in order.
func (o *Obj) Pairs() []Pair { return o.pairs }

// MarshalJSON encodes the object with its keys in insertion order.
func (o *Obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range o.pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(p.Key)
		if err != nil {
			return nil, err
		}
		v, err := encodeValue(p.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// counter counts strings and remembers first-seen order.
type counter struct {
	order []string
	n     map[string]int64
}

func newCounter() *counter { return &counter{n: map[string]int64{}} }

func (c *counter) add(key string) {
	if _, seen := c.n[key]; !seen {
		c.order = append(c.order, key)
	}
	c.n[key]++
}

// obj lists the counts in first-seen order, or by descending count (ties in
// first-seen order) when mostCommon is set.
func (c *counter) obj(mostCommon bool) *Obj {
	keys := append([]string(nil), c.order...)
	if mostCommon {
		sort.SliceStable(keys, func(i, j int) bool { return c.n[keys[i]] > c.n[keys[j]] })
	}
	o := &Obj{}
	for _, k := range keys {
		o.Set(k, c.n[k])
	}
	return o
}

// entry is the part of a folded run the statistics read.
type entry struct {
	source   string
	hook     bool
	status   string
	outcome  string
	scenario string
	provider string
	ts       string
	exitCode *int64
	outBytes *int64
	duration *int64
	findings map[string]any
	usage    map[string]any
	hasUsage bool
}

func newEntry(run map[string]any) entry {
	e := entry{source: "unknown", provider: "unknown"}
	if s, ok := run["source"].(string); ok {
		e.source = s
	}
	e.hook = strings.HasPrefix(e.source, "hook")
	e.outcome, _ = run["outcome"].(string)
	e.scenario, _ = run["scenario"].(string)
	if p, ok := run["provider"].(string); ok && p != "" {
		e.provider = p
	}
	e.ts, _ = run["ts"].(string)
	e.exitCode = intPtr(run["exit_code"])
	e.outBytes = intPtr(run["output_bytes"])
	e.duration = intPtr(run["duration_ms"])
	e.usage, e.hasUsage = run["usage"].(map[string]any)

	attrs, _ := run["attrs"].(map[string]any)
	if legacy, _ := attrs["legacy"].(bool); legacy {
		// Imported records keep the status they had.
		e.status, _ = run["status"].(string)
	} else {
		e.status = statusFromOutcome(e.outcome)
	}
	if f, ok := attrs["review.findings"].(map[string]any); ok {
		e.findings = f
	} else if f, ok := run["findings"].(map[string]any); ok {
		e.findings = f
	}
	return e
}

// statusFromOutcome derives the legacy-style status of a native run.
func statusFromOutcome(outcome string) string {
	switch outcome {
	case "error", "timeout", "cancelled", "lost":
		return "error"
	case "empty":
		return "empty-output"
	}
	return "ok"
}

func intPtr(v any) *int64 {
	n, ok := asInt(v)
	if !ok {
		return nil
	}
	return &n
}

// asInt coerces a telemetry number the way the retired analyzer did: integers
// as they are, floats truncated, numeric strings parsed, anything else (null,
// booleans, objects) not a number.
func asInt(v any) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, true
		}
		if f, err := x.Float64(); err == nil {
			return int64(f), true
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// ComputeStats summarizes folded runs. windowDays is nil for an unbounded
// read. The keys are the retired analyzer's, with by_provider, usage_totals
// and skipped added.
func ComputeStats(runs []map[string]any, skipped Skipped, windowDays *int) *Obj {
	entries := make([]entry, len(runs))
	providers := newCounter()
	for i, r := range runs {
		entries[i] = newEntry(r)
		providers.add(entries[i].provider)
	}
	out := &Obj{}
	out.Set("total", int64(len(entries)))
	if len(entries) == 0 {
		out.Set("empty", true)
	} else {
		out.Set("empty", false)
		out.Set("span", spanOf(entries))
		bySource := newCounter()
		bySourceStatus := map[string]*counter{}
		var sourceOrder []string
		withStatus := 0
		var hooks []entry
		var durations []int64
		for _, e := range entries {
			bySource.add(e.source)
			if bySourceStatus[e.source] == nil {
				bySourceStatus[e.source] = newCounter()
				sourceOrder = append(sourceOrder, e.source)
			}
			st := e.status
			if st == "" {
				st = "no-status"
			} else {
				withStatus++
			}
			bySourceStatus[e.source].add(st)
			if e.hook {
				hooks = append(hooks, e)
			}
			if e.duration != nil {
				durations = append(durations, *e.duration)
			}
		}
		out.Set("by_source", bySource.obj(true))
		statusObj := &Obj{}
		for _, src := range sourceOrder {
			statusObj.Set(src, bySourceStatus[src].obj(false))
		}
		out.Set("by_source_status", statusObj)
		out.Set("instrumentation", obj("with_status", int64(withStatus), "missing_status", int64(len(entries)-withStatus)))
		out.Set("reliability", reliability(hooks))
		out.Set("outcomes", outcomes(hooks))
		out.Set("duration_ms", percentiles(durations))
		out.Set("findings", findingsSummary(entries))
		out.Set("review_findings_proxy", findingsProxy(entries))
	}
	if windowDays == nil {
		out.Set("window_days", nil)
	} else {
		out.Set("window_days", int64(*windowDays))
	}
	out.Set("by_provider", providers.obj(true))
	out.Set("usage_totals", usageTotals(entries))
	out.Set("skipped", skipped)
	return out
}

func obj(kv ...any) *Obj {
	o := &Obj{}
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func spanOf(entries []entry) *Obj {
	var stamps []string
	for _, e := range entries {
		if e.ts != "" {
			stamps = append(stamps, e.ts)
		}
	}
	if len(stamps) == 0 {
		return obj("first", nil, "last", nil)
	}
	sort.Strings(stamps)
	return obj("first", stamps[0], "last", stamps[len(stamps)-1])
}

func percentiles(values []int64) *Obj {
	if len(values) == 0 {
		return obj("count", int64(0), "min", nil, "median", nil, "max", nil)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return obj("count", int64(len(values)), "min", values[0], "median", values[len(values)/2], "max", values[len(values)-1])
}

func reliability(hooks []entry) *Obj {
	var with, ok, bad, empty int64
	for _, e := range hooks {
		if e.status == "" {
			continue
		}
		with++
		switch e.status {
		case "ok":
			ok++
		case "error":
			bad++
		case "empty-output":
			empty++
		}
	}
	return obj("with_status", with, "ok", ok, "error", bad, "empty_output", empty)
}

func outcomes(hooks []entry) *Obj {
	c := newCounter()
	for _, e := range hooks {
		o := e.outcome
		if o == "" {
			o = "no-outcome"
		}
		c.add(o)
	}
	return c.obj(true)
}

func isHookReview(e entry) bool { return e.scenario == "code-review" && e.hook }

func findingsSummary(entries []entry) *Obj {
	var reviews []entry
	for _, e := range entries {
		if isHookReview(e) && e.findings != nil {
			reviews = append(reviews, e)
		}
	}
	if len(reviews) == 0 {
		return obj("instrumented_reviews", int64(0))
	}
	totals := &Obj{}
	for _, key := range []string{"total", "critical", "high", "medium", "low", "violations"} {
		var sum int64
		for _, e := range reviews {
			n, _ := asInt(e.findings[key])
			sum += n
		}
		totals.Set(key, sum)
	}
	var withFindings int64
	for _, e := range reviews {
		if n, _ := asInt(e.findings["total"]); n > 0 {
			withFindings++
		}
	}
	return obj("instrumented_reviews", int64(len(reviews)), "reviews_with_findings", withFindings,
		"reviews_clean", int64(len(reviews))-withFindings, "severity_totals", totals)
}

func findingsProxy(entries []entry) *Obj {
	type review struct {
		bytes int64
		ok    bool
	}
	var reviews []review
	for _, e := range entries {
		if !isHookReview(e) || e.outBytes == nil {
			continue
		}
		succeeded := (e.exitCode == nil || *e.exitCode == 0) && e.status != "error" && e.status != "empty-output"
		reviews = append(reviews, review{*e.outBytes, succeeded})
	}
	if len(reviews) == 0 {
		return obj("total", int64(0))
	}
	var sizes []int64
	var clean, withOutput int64
	for _, r := range reviews {
		if !r.ok {
			continue
		}
		sizes = append(sizes, r.bytes)
		if r.bytes <= cleanOutputMaxBytes {
			clean++
		} else {
			withOutput++
		}
	}
	failed := int64(len(reviews) - len(sizes))
	if len(sizes) == 0 {
		return obj("total", int64(len(reviews)), "succeeded", int64(0), "failed_or_empty", failed)
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] < sizes[j] })
	return obj("total", int64(len(reviews)), "succeeded", int64(len(sizes)), "failed_or_empty", failed,
		"clean_proxy", clean, "with_output_proxy", withOutput, "median_output_bytes", sizes[len(sizes)/2], "note", proxyNote)
}

func usageTotals(entries []entry) *Obj {
	keys := []string{"input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens", "reasoning_output_tokens"}
	sums := make([]int64, len(keys))
	var runs int64
	for _, e := range entries {
		if !e.hasUsage {
			continue
		}
		runs++
		for i, k := range keys {
			n, _ := asInt(e.usage[k])
			sums[i] += n
		}
	}
	o := &Obj{}
	for i, k := range keys {
		o.Set(k, sums[i])
	}
	o.Set("runs_with_usage", runs)
	return o
}
