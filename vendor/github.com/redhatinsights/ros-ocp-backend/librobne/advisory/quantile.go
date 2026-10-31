// Verbatim port of the server's histogram math
// (internal/engine/correlate/quantile.go) — the shared, pure, unit-tested
// path both the H leg (#723) and the api-tax rule (#724) compute p99s
// through. Ported, not reimplemented: any drift here is a parity bug by
// definition.
package advisory

import (
	"math"
	"slices"
	"time"
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

// Sample is one stored cumulative bucket observation, shaped for
// WindowP99 (the server's bucketSample; Group carries the verb group or
// the webhook name).
type Sample struct {
	Group     string
	LE        float64
	Count     float64
	Collected time.Time
}

// WindowP99 computes the p99 per group over one window from cumulative
// snapshots (the server's windowP99, ported verbatim). The delta baseline
// is the latest snapshot strictly before window start (bounded to 2h back
// for hourly cycles). minSpan gates the time between the two snapshots
// (the 30m min-data rule); 0 disables the span gate but never the
// two-snapshot requirement. Unknown on any gap.
func WindowP99(all []Sample, windowStart, windowEnd time.Time, minSpan time.Duration) (map[string]float64, bool) {
	type key struct {
		group string
		le    float64
	}
	latest := make(map[key]Sample)
	latestBefore := make(map[key]Sample)
	for _, s := range all {
		k := key{s.Group, s.LE}
		inWindow := !s.Collected.Before(windowStart) && !s.Collected.After(windowEnd)
		if inWindow {
			if prev, ok := latest[k]; !ok || s.Collected.After(prev.Collected) {
				latest[k] = s
			}
			continue
		}
		if s.Collected.Before(windowStart) && !s.Collected.Before(windowStart.Add(-2*time.Hour)) {
			if prev, ok := latestBefore[k]; !ok || s.Collected.After(prev.Collected) {
				latestBefore[k] = s
			}
		}
	}
	byGroup := make(map[string][]Bucket)
	prevByGroup := make(map[string][]Bucket)
	var newest, oldest time.Time
	first := true
	for k, s := range latest {
		p, ok := latestBefore[k]
		if !ok {
			continue
		}
		byGroup[s.Group] = append(byGroup[s.Group], Bucket{LE: s.LE, Count: s.Count})
		prevByGroup[s.Group] = append(prevByGroup[s.Group], Bucket{LE: p.LE, Count: p.Count})
		if first || s.Collected.After(newest) {
			newest = s.Collected
		}
		if first || p.Collected.Before(oldest) {
			oldest = p.Collected
		}
		first = false
	}
	if len(byGroup) == 0 || newest.Sub(oldest) < minSpan {
		return nil, false
	}
	out := make(map[string]float64, len(byGroup))
	for g, cur := range byGroup {
		p99, ok := Quantile(Deltas(prevByGroup[g], cur), 0.99)
		if !ok {
			return nil, false
		}
		out[g] = p99
	}
	return out, len(out) > 0
}
