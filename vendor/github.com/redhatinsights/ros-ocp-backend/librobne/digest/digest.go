package digest

import (
	"cmp"
	"math"
	"slices"
	"sync"
)

// Digest holds exact percentiles and aggregates for one in-memory sample series.
type Digest struct {
	P50   int64
	P60   int64
	P95   int64
	P98   int64
	P99   int64
	Max   int64
	Mean  int64
	Sum   int64
	Count int64
}

// ComputeDigest computes exact percentiles and aggregates from a sorted slice
// of int64 values. Uses slices.Sort() for O(n log n) exact computation.
// The input slice is sorted in place.
func ComputeDigest(values []int64) Digest {
	n := len(values)
	if n == 0 {
		return Digest{}
	}

	slices.Sort(values)

	var sum int64
	for _, v := range values {
		sum += v
	}

	return Digest{
		P50:   PercentileFromSorted(values, 0.50),
		P60:   PercentileFromSorted(values, 0.60),
		P95:   PercentileFromSorted(values, 0.95),
		P98:   PercentileFromSorted(values, 0.98),
		P99:   PercentileFromSorted(values, 0.99),
		Max:   values[n-1],
		Mean:  sum / int64(n),
		Sum:   sum,
		Count: int64(n),
	}
}

// percentileFromSorted returns the value at the given percentile from a
// pre-sorted slice using the nearest-lower-rank method: index = floor(pct * (n-1)).
func PercentileFromSorted(sorted []int64, pct float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := int(pct * float64(n-1))
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

type weightedPair struct {
	v int64
	w int64 // weight scaled by weightScale (10000 = 1.0)
}

const weightScale int64 = 10000

const weightedCountingSortMaxSpan = 4096

const (
	maxWeightedPairsCap  = 512
	resetWeightedPairCap = 128
)

type weightedDigestScratch struct {
	pairs  []weightedPair
	counts []int
	sorted []weightedPair
}

var weightedDigestScratchPool = sync.Pool{
	New: func() any {
		return &weightedDigestScratch{
			pairs: make([]weightedPair, 0, 128),
		}
	},
}

func capWeightedPairs(pairs []weightedPair) []weightedPair {
	if cap(pairs) > maxWeightedPairsCap {
		return make([]weightedPair, 0, resetWeightedPairCap)
	}
	return pairs
}

// ComputeWeightedDigest computes percentiles using per-sample weights.
// Samples with weight <= 0 are excluded. When all retained weights are 1.0,
// results match [ComputeDigest] on the same values.
func ComputeWeightedDigest(values []int64, weights []float64) Digest {
	n := len(values)
	if n == 0 || len(weights) != n {
		return Digest{}
	}

	scratch := weightedDigestScratchPool.Get().(*weightedDigestScratch)
	pairs := scratch.pairs[:0]
	if cap(pairs) < n {
		pairs = make([]weightedPair, 0, n)
	}
	for i := range values {
		if weights[i] > 0 {
			scaledW := int64(math.Round(weights[i] * float64(weightScale)))
			if scaledW > 0 {
				pairs = append(pairs, weightedPair{values[i], scaledW})
			}
		}
	}
	pn := len(pairs)
	if pn == 0 {
		scratch.pairs = capWeightedPairs(pairs)
		weightedDigestScratchPool.Put(scratch)
		return Digest{}
	}

	sortWeightedPairs(scratch, pairs)

	var sum int64
	var weightedSum int64
	var totalWeight int64
	allOnes := true
	for _, p := range pairs {
		sum += p.v
		weightedSum += p.v * p.w
		totalWeight += p.w
		if p.w != weightScale {
			allOnes = false
		}
	}

	mean := int64(0)
	if totalWeight > 0 {
		if allOnes {
			mean = sum / int64(pn)
		} else {
			mean = (weightedSum + totalWeight/2) / totalWeight
		}
	}

	var p50, p60, p95, p98, p99 int64
	if allOnes {
		p50 = percentileFromWeightedPairs(pairs, 0.50)
		p60 = percentileFromWeightedPairs(pairs, 0.60)
		p95 = percentileFromWeightedPairs(pairs, 0.95)
		p98 = percentileFromWeightedPairs(pairs, 0.98)
		p99 = percentileFromWeightedPairs(pairs, 0.99)
	} else {
		p50, p60, p95, p98, p99 = weightedPercentilesFromPairs(pairs, totalWeight)
	}

	scratch.pairs = capWeightedPairs(pairs)
	weightedDigestScratchPool.Put(scratch)

	return Digest{
		P50:   p50,
		P60:   p60,
		P95:   p95,
		P98:   p98,
		P99:   p99,
		Max:   pairs[pn-1].v,
		Mean:  mean,
		Sum:   sum,
		Count: int64(pn),
	}
}

func sortWeightedPairs(scratch *weightedDigestScratch, pairs []weightedPair) {
	pn := len(pairs)
	if pn <= 1 {
		return
	}
	minV, maxV := pairs[0].v, pairs[0].v
	for _, p := range pairs[1:] {
		if p.v < minV {
			minV = p.v
		}
		if p.v > maxV {
			maxV = p.v
		}
	}
	span := int(maxV - minV + 1)
	if span > weightedCountingSortMaxSpan {
		slices.SortFunc(pairs, func(a, b weightedPair) int {
			return cmp.Compare(a.v, b.v)
		})
		return
	}

	counts := scratch.counts
	if cap(counts) < span {
		counts = make([]int, span)
	} else {
		counts = counts[:span]
		clear(counts)
	}
	for _, p := range pairs {
		counts[p.v-minV]++
	}
	pos := 0
	for i := range counts {
		c := counts[i]
		counts[i] = pos
		pos += c
	}

	sorted := scratch.sorted
	if cap(sorted) < pn {
		sorted = make([]weightedPair, pn)
	} else {
		sorted = sorted[:pn]
	}
	for _, p := range pairs {
		idx := counts[p.v-minV]
		sorted[idx] = p
		counts[p.v-minV]++
	}
	copy(pairs, sorted)

	scratch.counts = counts
	scratch.sorted = sorted
}

func percentileFromWeightedPairs(pairs []weightedPair, pct float64) int64 {
	n := len(pairs)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return pairs[0].v
	}
	rank := int(pct * float64(n-1))
	if rank >= n {
		rank = n - 1
	}
	return pairs[rank].v
}

// weightedPercentilesFromPairs returns p50, p60, p95, p98, p99 in one cumulative-weight pass.
func weightedPercentilesFromPairs(pairs []weightedPair, totalWeight int64) (p50, p60, p95, p98, p99 int64) {
	if totalWeight <= 0 {
		return 0, 0, 0, 0, 0
	}
	n := len(pairs)
	if n == 1 {
		v := pairs[0].v
		return v, v, v, v, v
	}

	targets := [5]int64{
		(50 * totalWeight) / 100,
		(60 * totalWeight) / 100,
		(95 * totalWeight) / 100,
		(98 * totalWeight) / 100,
		(99 * totalWeight) / 100,
	}
	results := [5]int64{}
	last := pairs[n-1].v
	next := 0
	cum := int64(0)
	for i := range pairs {
		cum += pairs[i].w
		for next < 5 && cum >= targets[next] {
			results[next] = pairs[i].v
			next++
		}
	}
	for next < 5 {
		results[next] = last
		next++
	}
	return results[0], results[1], results[2], results[3], results[4]
}

func WeightedPercentileFromSorted(sorted []int64, weights []float64, pct float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}

	allOnes := true
	for _, w := range weights {
		if w != 1.0 {
			allOnes = false
			break
		}
	}
	if allOnes {
		return PercentileFromSorted(sorted, pct)
	}

	var total float64
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return 0
	}
	target := pct * total
	cum := 0.0
	for i, w := range weights {
		cum += w
		if cum >= target {
			return sorted[i]
		}
	}
	return sorted[n-1]
}
