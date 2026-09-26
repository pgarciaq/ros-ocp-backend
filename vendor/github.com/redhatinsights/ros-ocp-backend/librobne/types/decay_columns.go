package types

import (
	"math"
	"time"
)

// ColumnWindowOpts configures idle detection and trend slope for
// MultiWeightedPercentileColumns. It is the Column-shaped counterpart of
// WindowExtraOpts: a zero value means "don't track this signal", which is what a
// nil func meant there.
type ColumnWindowOpts struct {
	// TrendColumn and MemTrendColumn name the series each trend slope is
	// regressed against. ColNone disables that trend.
	TrendColumn    Column
	MemTrendColumn Column

	IdleThresholdMC  int64
	IdleThresholdMem int64
	DetectIdle       bool
}

// MultiWeightedPercentileColumns computes decay-weighted averages of the named
// columns in one pass over rows, plus optional idle flag and trend slopes.
//
// It is the allocation-free counterpart of MultiWeightedPercentileWithExtras:
// same arithmetic in the same order, but columns are uint8 descriptors instead
// of closures, so the extractor list is plain stack data and each column is read
// in place from the row the walk already holds rather than copied per column.
// For the same rows, window, half-life, and equivalent opts, the two functions
// return identical values — a property the tests assert directly rather than
// leave to review, because both walks now exist (the closure form is retained
// for the internal/engine compat bridge, whose signatures are frozen until its
// consumers migrate).
func MultiWeightedPercentileColumns(
	rows []DigestRow,
	now time.Time,
	halfLifeHours float64,
	opts *ColumnWindowOpts,
	columns ...Column,
) ([]int64, WindowExtras) {
	nOut := len(columns)
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
	var sumYMem, sumXYMem float64
	trackTrend := opts != nil && opts.TrendColumn != ColNone
	trackMemTrend := opts != nil && opts.MemTrendColumn != ColNone
	n := len(rows)

	for i := range rows {
		row := &rows[i]
		if opts != nil && opts.DetectIdle {
			if row.CPUUsageMaxMC >= opts.IdleThresholdMC || row.MemUsageMaxKiB >= opts.IdleThresholdMem {
				extras.IsIdle = false
			}
		}
		if trackTrend || trackMemTrend {
			x := float64(i)
			sumX += x
			sumX2 += x * x
			if trackTrend {
				y := float64(opts.TrendColumn.Value(row))
				sumY += y
				sumXY += x * y
			}
			if trackMemTrend {
				yMem := float64(opts.MemTrendColumn.Value(row))
				sumYMem += yMem
				sumXYMem += x * yMem
			}
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
		for j, col := range columns {
			weightedSums[j] += float64(col.Value(row)) * w
		}
	}

	if n >= 2 {
		nf := float64(n)
		denom := nf*sumX2 - sumX*sumX
		if denom != 0 {
			if trackTrend {
				extras.TrendSlope = (nf*sumXY - sumX*sumY) / denom
			}
			if trackMemTrend {
				extras.MemTrendSlope = (nf*sumXYMem - sumX*sumYMem) / denom
			}
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
