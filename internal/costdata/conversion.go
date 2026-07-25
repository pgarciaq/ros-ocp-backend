package costdata

import "math"

// ConvertCents converts a cents value from one currency to another using a
// multiplication rate. Uses round-half-up (math.Floor(x + 0.5)) consistent
// with Koku's rounding behaviour.
func ConvertCents(cents int64, rate float64) int64 {
	return int64(math.Floor(float64(cents)*rate + 0.5))
}
