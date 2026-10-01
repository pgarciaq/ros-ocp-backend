package correlate

import (
	"math"
	"slices"
)

// Bucket is one histogram bucket with a cumulative count. LE is +Inf for the
// overflow bucket, which Prometheus histograms always carry and quantile
// computation requires.
type Bucket struct {
	LE    float64
	Count float64
}

// Deltas converts consecutive cumulative snapshots to per-window counts,
// keyed by LE. Counter decreases (resets, restarts) resolve to the current
// value — the same reset-awareness as Prometheus rate(). Missing keys on
// either side resolve to the present side. Pure and unit-tested.
func Deltas(prev, cur []Bucket) []Bucket {
	before := make(map[float64]float64, len(prev))
	for _, b := range prev {
		before[b.LE] = b.Count
	}
	seen := make(map[float64]bool, len(cur))
	out := make([]Bucket, 0, len(cur))
	for _, b := range cur {
		if seen[b.LE] {
			continue
		}
		seen[b.LE] = true
		d := b.Count
		if p, ok := before[b.LE]; ok && b.Count >= p {
			d = b.Count - p
		}
		if d < 0 {
			d = 0
		}
		out = append(out, Bucket{LE: b.LE, Count: d})
	}
	slices.SortFunc(out, func(a, b Bucket) int {
		switch {
		case a.LE < b.LE:
			return -1
		case a.LE > b.LE:
			return 1
		default:
			return 0
		}
	})
	return out
}

// Quantile estimates the q-quantile (e.g. 0.99) over per-window bucket
// deltas with Prometheus histogram_quantile semantics: linear interpolation
// within the first bucket whose cumulative count reaches q * total.
// Decreases are clamped to the running maximum (the lab's monotonicity fix:
// scraped bucket series are not guaranteed monotonic). Empty or all-zero
// input yields ok=false — callers treat that as unknown, never zero pain.
func Quantile(buckets []Bucket, q float64) (float64, bool) {
	if len(buckets) == 0 {
		return 0, false
	}
	sorted := append([]Bucket(nil), buckets...)
	slices.SortFunc(sorted, func(a, b Bucket) int {
		switch {
		case a.LE < b.LE:
			return -1
		case a.LE > b.LE:
			return 1
		default:
			return 0
		}
	})
	total := 0.0
	running := make([]float64, len(sorted))
	peak := 0.0
	for i, b := range sorted {
		c := b.Count
		if c < 0 {
			c = 0
		}
		if c < peak {
			c = peak
		}
		peak = c
		running[i] = c
	}
	total = peak
	if total <= 0 {
		return 0, false
	}
	rank := q * total
	prevLE, prevCum := 0.0, 0.0
	for i, b := range sorted {
		cum := running[i]
		if cum >= rank {
			if math.IsInf(b.LE, 1) {
				// Tail beyond the highest finite bound: report that bound
				// rather than +Inf (Prometheus-equivalent finite answer).
				return prevLE, true
			}
			width := b.LE - prevLE
			span := cum - prevCum
			if span <= 0 {
				return b.LE, true
			}
			return prevLE + width*(rank-prevCum)/span, true
		}
		prevLE, prevCum = b.LE, cum
	}
	return sorted[len(sorted)-1].LE, true
}

// MedianFloat64 returns the median, or false on empty input.
func MedianFloat64(vals []float64) (float64, bool) {
	if len(vals) == 0 {
		return 0, false
	}
	cp := append([]float64(nil), vals...)
	slices.Sort(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid], true
	}
	return (cp[mid-1] + cp[mid]) / 2, true
}
