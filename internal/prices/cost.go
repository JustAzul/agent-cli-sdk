package prices

import "math/big"

// Price is what one model costs in US dollars per million tokens, already
// resolved: a cache-read or cache-write price the source omitted is stored as
// the input price.
type Price struct{ Input, CachedInput, CacheWrite, Output *big.Rat }

// Usage is the token counts of one run. A negative count is read as 0.
type Usage struct{ Input, Cached, CacheWrite, Output int64 }

var million = big.NewRat(1000000, 1)

// RunCost is the cost in US dollars of u at price p, exact and unrounded.
//
// A provider that reports no cache writes is taken to have written every
// uncached input token, as implicit prompt caching does; reported writes win.
// Reasoning tokens are part of Output and are not counted again.
func RunCost(u Usage, p Price) *big.Rat {
	input, cached, output := max(u.Input, 0), max(u.Cached, 0), max(u.Output, 0)
	written := max(u.CacheWrite, 0)
	if written == 0 {
		written = max(input-cached, 0)
	}
	ordinary := max(input-cached-written, 0)

	total := new(big.Rat)
	for _, term := range []struct {
		tokens int64
		price  *big.Rat
	}{{ordinary, p.Input}, {cached, p.CachedInput}, {written, p.CacheWrite}, {output, p.Output}} {
		total.Add(total, new(big.Rat).Mul(new(big.Rat).SetInt64(term.tokens), term.price))
	}
	return total.Quo(total, million)
}
