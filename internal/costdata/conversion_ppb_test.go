package costdata

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// #462: the FX conversion is integer ppb arithmetic, not float64. Masu sends
// the rate as a JSON string to keep Decimal digits; ROS parses it to integer
// parts-per-billion and converts with round-half-up, using a decomposition
// that cannot overflow int64 (the naive cents×ppb product overflows above
// ~$92M per amount — fleet totals reach that class).

func TestParseRateStringToPPB(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0.860000000000000", 860_000_000},
		{"1", 1_000_000_000},
		{"1.0", 1_000_000_000},
		{"150.123456789", 150_123_456_789},
		{"0.000000001", 1},
		{"0", 0},
		{"0.5", 500_000_000},
	}
	for _, tc := range cases {
		got, err := ParseRateStringToPPB(tc.in)
		assert.NoError(t, err, "parse %q", tc.in)
		assert.Equal(t, tc.want, got, "parse %q", tc.in)
	}

	_, err := ParseRateStringToPPB("not-a-number")
	assert.Error(t, err, "garbage must error, not silently become a rate")
}

func TestConvertCentsPPB(t *testing.T) {
	// Round-half-up on the 9th ppb digit, matching the previous
	// floor(cents*rate + 0.5) semantics for non-negative cents.
	assert.Equal(t, int64(86), ConvertCentsPPB(100, 860_000_000),
		"100 cents at 0.86 = 86")
	assert.Equal(t, int64(43), ConvertCentsPPB(50, 860_000_000),
		"50 cents at 0.86 = 43 (0.5 rounds up)")
	assert.Equal(t, int64(42), ConvertCentsPPB(49, 860_000_000),
		"49 cents at 0.86 = 42.14 -> 42")
	assert.Equal(t, int64(100), ConvertCentsPPB(100, 1_000_000_000),
		"identity rate")
	assert.Equal(t, int64(100), ConvertCentsPPB(100, 0),
		"ppb <= 0: no conversion (guard)")
	assert.Equal(t, int64(100), ConvertCentsPPB(100, -5),
		"negative ppb: no conversion (guard)")

	t.Run("overflow class: the naive product would overflow int64", func(t *testing.T) {
		// $92M = 9.2e9 hundredth-cents; x 860e6 ppb = 7.9e18 — near int64
		// max (9.2e18). The naive (cents*ppb + 5e8)/1e9 overflows above
		// ~$10.7M at this rate... no: 9.2e18/860e6 ~ 1.07e13 cents ~ $107B.
		// Use a value that DOES overflow the naive product:
		cents := int64(9_200_000_000_000) // $92B — 9.2e12 cents
		ppb := int64(860_000_000)
		naive := cents * ppb // overflows int64? 9.2e12 * 8.6e8 = 7.9e21 > 9.2e18 — yes
		assert.NotEqual(t, int64(float64(cents)*float64(ppb)), naive,
			"precondition: the naive product is corrupt (overflowed)")

		got := ConvertCentsPPB(cents, ppb)
		want := int64(math.Round(float64(cents) * 0.86))
		assert.Equal(t, want, got,
			"$92B at 0.86 must convert exactly despite the naive product overflowing")
	})

	t.Run("negative cents (additional-cost rows): round-half-away-from-zero", func(t *testing.T) {
		// The previous float path used floor(x+0.5) (half-up toward +inf);
		// the integer path mirrors it via sign decomposition. The .5-negative
		// edge differs by one cent from the float path — recorded.
		assert.Equal(t, int64(-43), ConvertCentsPPB(-50, 860_000_000),
			"-50 cents at 0.86 = -43")
	})
}
