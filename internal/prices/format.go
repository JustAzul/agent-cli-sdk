package prices

import (
	"errors"
	"math/big"
	"strings"
)

var (
	bigTwo  = big.NewInt(2)
	bigFive = big.NewInt(5)
	bigTen  = big.NewInt(10)
)

// FormatUSD prints r with exactly six decimals, rounding half to even.
func FormatUSD(r *big.Rat) string {
	return formatScaled(scaledHalfEven(r, 6), 6)
}

// FormatCents prints r with exactly two decimals, rounding half up.
func FormatCents(r *big.Rat) string {
	scaled := new(big.Rat).Mul(r, big.NewRat(100, 1))
	scaled.Add(scaled, big.NewRat(1, 2))
	return formatScaled(floorRat(scaled), 2)
}

// FormatDecimal prints r as its shortest exact decimal: no exponent, no
// trailing zeros after the point and no point for a whole number. It fails
// when the decimal expansion of r does not terminate.
func FormatDecimal(r *big.Rat) (string, error) {
	rest := new(big.Int).Set(r.Denom())
	twos, fives := stripFactor(rest, bigTwo), stripFactor(rest, bigFive)
	if rest.Cmp(big.NewInt(1)) != 0 {
		return "", errors.New("price has no exact decimal form")
	}
	return r.FloatString(max(twos, fives)), nil
}

// stripFactor divides f out of n as many times as it divides and returns how
// many times it did.
func stripFactor(n, f *big.Int) int {
	count := 0
	q, m := new(big.Int), new(big.Int)
	for {
		q.QuoRem(n, f, m)
		if m.Sign() != 0 {
			return count
		}
		n.Set(q)
		count++
	}
}

// scaledHalfEven is r×10^scale rounded to an integer, ties to the even one.
func scaledHalfEven(r *big.Rat, scale int) *big.Int {
	x := new(big.Rat).Mul(r, new(big.Rat).SetInt(pow10(scale)))
	q, rem := new(big.Int).QuoRem(x.Num(), x.Denom(), new(big.Int))
	// QuoRem truncates toward zero; rem has the sign of the numerator.
	twice := new(big.Int).Abs(rem)
	twice.Lsh(twice, 1)
	cmp := twice.Cmp(x.Denom())
	if cmp > 0 || cmp == 0 && q.Bit(0) == 1 {
		if x.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}

// floorRat is the greatest integer not above x.
func floorRat(x *big.Rat) *big.Int {
	q, rem := new(big.Int).QuoRem(x.Num(), x.Denom(), new(big.Int))
	if rem.Sign() < 0 {
		q.Sub(q, big.NewInt(1))
	}
	return q
}

func pow10(n int) *big.Int { return new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil) }

// formatScaled prints n/10^scale with exactly scale decimals.
func formatScaled(n *big.Int, scale int) string {
	neg := n.Sign() < 0
	digits := new(big.Int).Abs(n).String()
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	out := digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out
}
