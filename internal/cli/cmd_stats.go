package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JustAzul/agent-cli-sdk/internal/telemetry"
)

func init() {
	register(Command{Name: "stats", Summary: "summarize telemetry (reliability, outcomes, findings, usage)", Run: runStats})
}

func runStats(ctx *Context, args []string) int {
	w, asJSON, exit, done := windowFlags(ctx, "stats", args)
	if done {
		return exit
	}
	ctx.JSON = asJSON
	folded, code := foldWindow(ctx, w)
	if code != 0 {
		return code
	}
	var days *int
	if !w.all {
		days = &w.days
	}
	stats := telemetry.ComputeStats(folded.Runs, folded.Skipped, days)
	if asJSON {
		data, err := json.MarshalIndent(stats, "", "  ")
		if err != nil {
			return ctx.Fail(ExitInternal, "encoding stats: %v", err)
		}
		fmt.Fprintf(ctx.Stdout, "%s\n", data)
		return ExitOK
	}
	printStatsSummary(ctx, stats, folded.Skipped)
	return ExitOK
}

// printStatsSummary writes the short human-readable form of the statistics.
func printStatsSummary(ctx *Context, stats *telemetry.Obj, skipped telemetry.Skipped) {
	window := "all history"
	if d, ok := stats.Get("window_days").(int64); ok {
		window = fmt.Sprintf("last %d days", d)
	}
	fmt.Fprintf(ctx.Stdout, "runs: %d (%s)\n", stats.Get("total"), window)
	if stats.Get("empty") == false {
		if span, ok := stats.Get("span").(*telemetry.Obj); ok {
			fmt.Fprintf(ctx.Stdout, "span: %v .. %v\n", span.Get("first"), span.Get("last"))
		}
		fmt.Fprintf(ctx.Stdout, "by source: %s\n", countsLine(stats.Get("by_source")))
		fmt.Fprintf(ctx.Stdout, "by provider: %s\n", countsLine(stats.Get("by_provider")))
		fmt.Fprintf(ctx.Stdout, "hook outcomes: %s\n", countsLine(stats.Get("outcomes")))
		if d, ok := stats.Get("duration_ms").(*telemetry.Obj); ok && d.Get("count") != int64(0) {
			fmt.Fprintf(ctx.Stdout, "duration_ms: median %v (min %v, max %v)\n", d.Get("median"), d.Get("min"), d.Get("max"))
		}
		if f, ok := stats.Get("findings").(*telemetry.Obj); ok && f.Get("instrumented_reviews") != int64(0) {
			fmt.Fprintf(ctx.Stdout, "reviews with structured findings: %v (%v with findings)\n", f.Get("instrumented_reviews"), f.Get("reviews_with_findings"))
		}
	}
	if u, ok := stats.Get("usage_totals").(*telemetry.Obj); ok && u.Get("runs_with_usage") != int64(0) {
		fmt.Fprintf(ctx.Stdout, "tokens: %s (%v runs with usage)\n", countsLine(withoutKey(u, "runs_with_usage")), u.Get("runs_with_usage"))
	}
	if byModel, ok := stats.Get("usage_by_model").(*telemetry.Obj); ok {
		for _, p := range byModel.Pairs() {
			u := p.Value.(*telemetry.Obj)
			fmt.Fprintf(ctx.Stdout, "tokens %s: %s (%v runs)\n", p.Key, countsLine(withoutKey(u, "runs_with_usage")), u.Get("runs_with_usage"))
		}
	}
	if n := skipped.Total(); n > 0 {
		fmt.Fprintf(ctx.Stdout, "skipped lines: %d (unknown_kind=%d unknown_version=%d unparseable=%d)\n",
			n, skipped.UnknownKind, skipped.UnknownVersion, skipped.Unparseable)
	}
}

func withoutKey(o *telemetry.Obj, key string) *telemetry.Obj {
	out := &telemetry.Obj{}
	for _, p := range o.Pairs() {
		if p.Key != key {
			out.Set(p.Key, p.Value)
		}
	}
	return out
}

// countsLine renders an object of counts as "key=n key=n".
func countsLine(v any) string {
	o, ok := v.(*telemetry.Obj)
	if !ok || len(o.Pairs()) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(o.Pairs()))
	for _, p := range o.Pairs() {
		parts = append(parts, fmt.Sprintf("%s=%v", p.Key, p.Value))
	}
	return strings.Join(parts, " ")
}
