package snapshot

import "math"

// usdToCents converts a USD float amount to integer cents (rounded half away from zero).
// Copied from product internal/money so librobne does not import internal/.
func usdToCents(usd float64) int64 {
	return int64(math.Round(usd * 100))
}
