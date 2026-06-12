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
	// Quantize to integer hours for O(1) lookup from precomputed tables.
	ageInt := int(math.Round(ageHours))
	hlInt := int(math.Round(halfLifeHours))
	if hlInt <= 0 {
		return 1.0
	}
	// Integer half-lives (e.g. window_days*12) use the lookup table; others fall back.
	if float64(hlInt) == halfLifeHours {
		return decayTableLookup(ageInt, hlInt)
	}
	return math.Exp(-ageHours * math.Ln2 / halfLifeHours)
}

// WeightedPercentile computes a decay-weighted average of values extracted
// from DigestRows. More recent rows have higher weight.
// If halfLifeHours is 0, all rows have equal weight (simple average).
// Age is measured in continuous hours (see DecayWeight for rationale).
func WeightedPercentile(rows []DigestRow, now time.Time, halfLifeHours float64, pctFunc func(DigestRow) int64) int64 {
	results := MultiWeightedPercentile(rows, now, halfLifeHours, pctFunc)
	if len(results) == 0 {
		return 0
	}
	return results[0]
}

// WindowExtraOpts configures idle detection and trend slope computed in the
// same pass as MultiWeightedPercentile.
type WindowExtraOpts struct {
	TrendMetric      func(DigestRow) int64
	IdleThresholdMC  int64
	IdleThresholdMem int64
	DetectIdle       bool
}

// WindowExtras holds side computations from a fused digest window pass.
type WindowExtras struct {
	TrendSlope float64
	IsIdle     bool
}

// MultiWeightedPercentile computes several decay-weighted averages in one pass
// over rows, reusing decay weights for each extractor.
func MultiWeightedPercentile(rows []DigestRow, now time.Time, halfLifeHours float64, extractors ...func(DigestRow) int64) []int64 {
	out, _ := MultiWeightedPercentileWithExtras(rows, now, halfLifeHours, nil, extractors...)
	return out
}

// MultiWeightedPercentileWithExtras computes weighted percentiles plus optional
// idle flag and trend slope in a single row walk.
func MultiWeightedPercentileWithExtras(
	rows []DigestRow,
	now time.Time,
	halfLifeHours float64,
	opts *WindowExtraOpts,
	extractors ...func(DigestRow) int64,
) ([]int64, WindowExtras) {
	nOut := len(extractors)
	extras := WindowExtras{}
	if len(rows) == 0 || nOut == 0 {
		return make([]int64, nOut), extras
	}

	if opts != nil && opts.DetectIdle {
		extras.IsIdle = true
	}

	weightedSums := make([]float64, nOut)
	var totalWeight float64
	var sumX, sumY, sumXY, sumX2 float64
	trackTrend := opts != nil && opts.TrendMetric != nil
	n := len(rows)

	for i, row := range rows {
		if opts != nil && opts.DetectIdle {
			if row.CPUUsageMaxMC >= opts.IdleThresholdMC || row.MemUsageMaxKiB >= opts.IdleThresholdMem {
				extras.IsIdle = false
			}
		}
		if trackTrend {
			x := float64(i)
			y := float64(opts.TrendMetric(row))
			sumX += x
			sumY += y
			sumXY += x * y
			sumX2 += x * x
		}

		ageHours := now.Sub(row.BucketDate).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		w := DecayWeight(ageHours, halfLifeHours)
		if w == 0 {
			continue
		}
		totalWeight += w
		for j, extract := range extractors {
			weightedSums[j] += float64(extract(row)) * w
		}
	}

	if trackTrend && n >= 2 {
		nf := float64(n)
		denom := nf*sumX2 - sumX*sumX
		if denom != 0 {
			extras.TrendSlope = (nf*sumXY - sumX*sumY) / denom
		}
	}

	out := make([]int64, nOut)
	if totalWeight == 0 {
		return out, extras
	}
	for i := range out {
		out[i] = int64(math.Round(weightedSums[i] / totalWeight))
	}
	return out, extras
}
