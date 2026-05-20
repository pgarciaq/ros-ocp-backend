package engine

import (
	"math"
	"time"
)

// DecayWeight computes exponential decay: exp(-ageHours * ln(2) / halfLifeHours).
// Returns 1.0 if halfLifeHours is 0 or negative (no decay).
//
// NOTE: This uses continuous hour-based age, NOT calendar days. DST transitions
// or month boundaries may cause up to ~1h skew relative to calendar-day counting.
// This is intentional: continuous decay avoids jumps at midnight boundaries and
// provides smoother freshness scoring. The ~1h error on a typical 14-day window
// is negligible for recommendation quality scoring.
func DecayWeight(ageHours, halfLifeHours float64) float64 {
	if halfLifeHours <= 0 {
		return 1.0
	}
	return math.Exp(-ageHours * math.Ln2 / halfLifeHours)
}

// WeightedPercentile computes a decay-weighted average of values extracted
// from DigestRows. More recent rows have higher weight.
// If halfLifeHours is 0, all rows have equal weight (simple average).
// Age is measured in continuous hours (see DecayWeight for rationale).
func WeightedPercentile(rows []DigestRow, now time.Time, halfLifeHours float64, pctFunc func(DigestRow) int64) int64 {
	if len(rows) == 0 {
		return 0
	}

	var weightedSum, totalWeight float64
	for _, row := range rows {
		ageHours := now.Sub(row.BucketDate).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		w := DecayWeight(ageHours, halfLifeHours)
		val := float64(pctFunc(row))
		weightedSum += val * w
		totalWeight += w
	}

	if totalWeight == 0 {
		return 0
	}
	return int64(math.Round(weightedSum / totalWeight))
}
