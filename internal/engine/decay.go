package engine

import (
	"math"
	"time"
)

// DecayWeight computes exponential decay: exp(-ageHours * ln(2) / halfLifeHours).
// Returns 1.0 if halfLifeHours is 0 or negative (no decay).
func DecayWeight(ageHours, halfLifeHours float64) float64 {
	if halfLifeHours <= 0 {
		return 1.0
	}
	return math.Exp(-ageHours * math.Ln2 / halfLifeHours)
}

// WeightedPercentile computes a decay-weighted average of values extracted
// from DigestRows. More recent rows have higher weight.
// If halfLifeHours is 0, all rows have equal weight (simple average).
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
