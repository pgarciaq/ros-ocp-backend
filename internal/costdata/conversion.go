package costdata

import "math"

// ConvertCents converts a cents value from one currency to another using a
// multiplication rate. Uses round-half-up (math.Floor(x + 0.5)) consistent
// with Koku's rounding behaviour.
//
// If rate is zero or negative the original cents value is returned unchanged
// (identity operation), consistent with the upstream "default to 1.0 on error"
// fallback in fetchExchangeRate.
func ConvertCents(cents int64, rate float64) int64 {
	if rate <= 0 {
		return cents
	}
	return int64(math.Floor(float64(cents)*rate + 0.5))
}
