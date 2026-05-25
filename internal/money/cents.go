package money

import "math"

// USDToCents converts a USD float amount to integer cents (rounded half away from zero).
func USDToCents(usd float64) int64 {
	return int64(math.Round(usd * 100))
}

// CentsToUSD converts integer cents to USD float for API JSON responses.
func CentsToUSD(cents int64) float64 {
	return float64(cents) / 100.0
}

// CentsToUSDPtr converts nullable cents to nullable float32 USD for JSON responses.
func CentsToUSDPtr(cents *int64) *float32 {
	if cents == nil {
		return nil
	}
	v := float32(CentsToUSD(*cents))
	return &v
}

// CentsToFloat32 converts cents to float32 USD.
func CentsToFloat32(cents int64) float32 {
	return float32(CentsToUSD(cents))
}
