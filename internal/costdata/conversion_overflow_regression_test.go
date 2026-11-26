package costdata

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConvertCentsPPB_HighRateOracle is #948's regression: the b×ppb term
// silently overflows int64 for FX rates above ~9.2 (JPY ~150, HUF ~400,
// IDR ~15000) with remainders above 9.2e18/(rate×1e9) — every sub-9.2-rate
// fixture in #462's original suite missed the broken region, and the
// overflow-class fixture had b=0 by construction ($92B is a multiple of 1e9).
// The oracle is arbitrary-precision big.Int arithmetic.
func TestConvertCentsPPB_HighRateOracle(t *testing.T) {
	cases := []struct {
		cents int64
		rate  float64
		desc  string
	}{
		{100_000_000, 150.0, "$1M at JPY-rate 150"},
		{70_000_000, 150.0, "$700K at 150 (just over the cliff)"},
		{60_000_000, 150.0, "$600K at 150 (just under — was OK pre-fix too)"},
		{100_000_000, 400.0, "$1M at HUF-rate 400"},
		{1_000_000_007, 150.0, "$1M + 7 cents at 150 (non-trivial a AND b)"},
		{9_200_000_000, 0.86, "$92M at 0.86 (the v17 overflow-class fixture)"},
		{123_456_789, 1.111111111, "$1.23M at 1.11 (the lab's live pair)"},
		{50, 860_000_000.0 / 1e9, "tiny amount at 0.86 (rounding)"},
	}
	for _, tc := range cases {
		ppb := int64(tc.rate * 1e9)
		got := ConvertCentsPPB(tc.cents, ppb)
		want := new(big.Int).Div(
			new(big.Int).Add(
				new(big.Int).Mul(big.NewInt(tc.cents), big.NewInt(ppb)),
				big.NewInt(500_000_000),
			),
			big.NewInt(1_000_000_000),
		)
		if !want.IsInt64() {
			t.Fatalf("%s: oracle result exceeds int64 — fixture out of scope", tc.desc)
		}
		assert.Equal(t, want.Int64(), got,
			"%s: cents=%d ppb=%d must match the exact big.Int oracle (#948)", tc.desc, tc.cents, ppb)
	}
}

// TestConvertCentsPPB_MinInt64 is #945's regression: negating math.MinInt64
// overflows back to itself, so the negative branch recursed infinitely —
// a process-killing stack overflow (the v17 review's executed probe).
func TestConvertCentsPPB_MinInt64(t *testing.T) {
	// No stack overflow, no panic; the pathological amount passes through
	// unconverted (consistent with the ppb <= 0 guard's semantics).
	got := ConvertCentsPPB(math.MinInt64, 860_000_000)
	assert.Equal(t, int64(math.MinInt64), got,
		"MinInt64 cents: the guard must return without recursing (#945)")

	// The rest of the negative range still converts (round-half-away-from-zero).
	assert.Equal(t, int64(-43), ConvertCentsPPB(-50, 860_000_000),
		"ordinary negative cents still convert")
}
