package prices_test

import (
	"math/big"
	"testing"

	"github.com/JustAzul/agentcli/internal/prices"
)

func price(t *testing.T, input, cached, write, output string) prices.Price {
	t.Helper()
	return prices.Price{Input: rat(t, input), CachedInput: rat(t, cached), CacheWrite: rat(t, write), Output: rat(t, output)}
}

func TestRunCost(t *testing.T) {
	sol := price(t, "2", "0.1", "2.5", "10")
	old := price(t, "5", "0.5", "5", "30")
	bare := price(t, "1", "1", "1", "4")
	half := price(t, "0.5", "0.5", "0.5", "0.5")
	cases := []struct {
		name  string
		usage prices.Usage
		price prices.Price
		want  string // exact rational, USD
	}{
		{"estimated writes", prices.Usage{Input: 1000000, Cached: 800000, Output: 10000}, sol, "0.68"},
		{"reported writes win", prices.Usage{Input: 1000000, Cached: 800000, CacheWrite: 50000, Output: 10000}, sol, "0.605"},
		{"cache write price omitted", prices.Usage{Input: 100000, Cached: 60000, Output: 1000}, old, "0.26"},
		{"no cache fields", prices.Usage{Input: 10000, Cached: 4000, Output: 500}, bare, "0.012"},
		{"half a micro-dollar", prices.Usage{Input: 1}, half, "1/2000000"},
		{"no usage", prices.Usage{}, sol, "0"},
		{"negative counts read as zero", prices.Usage{Input: -5, Cached: -1, CacheWrite: -2, Output: -9}, sol, "0"},
		{"negative cached read as zero: all input estimated written", prices.Usage{Input: 1000000, Cached: -800000, Output: 10000}, sol, "2.6"},
		{"cached above input: no estimated writes, no negative input", prices.Usage{Input: 100, Cached: 300, Output: 0}, sol, "0.00003"},
		{"reported writes above uncached input", prices.Usage{Input: 100, Cached: 40, CacheWrite: 100}, sol, "0.000254"},
	}
	for _, c := range cases {
		got := prices.RunCost(c.usage, c.price)
		if got.Cmp(rat(t, c.want)) != 0 {
			t.Errorf("%s: cost %s, want %s", c.name, got.RatString(), c.want)
		}
	}
}

func TestRunCostSumsExactly(t *testing.T) {
	sol := prices.RunCost(prices.Usage{Input: 1000000, Cached: 800000, Output: 10000}, price(t, "2", "0.1", "2.5", "10"))
	old := prices.RunCost(prices.Usage{Input: 100000, Cached: 60000, Output: 1000}, price(t, "5", "0.5", "5", "30"))
	bare := prices.RunCost(prices.Usage{Input: 10000, Cached: 4000, Output: 500}, price(t, "1", "1", "1", "4"))
	total := new(big.Rat).Add(new(big.Rat).Add(sol, old), bare)
	if got := prices.FormatUSD(total); got != "0.952000" {
		t.Errorf("total %q, want 0.952000", got)
	}
}

func TestRunCostRoundsOnlyWhenPrinted(t *testing.T) {
	half := price(t, "0.5", "0.5", "0.5", "0.5")
	for _, c := range []struct {
		input int64
		want  string
	}{{1, "0.000000"}, {3, "0.000002"}} {
		got := prices.FormatUSD(prices.RunCost(prices.Usage{Input: c.input}, half))
		if got != c.want {
			t.Errorf("input %d: %q, want %q", c.input, got, c.want)
		}
	}
}
