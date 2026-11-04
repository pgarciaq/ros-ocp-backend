package digest

import (
	"sync"
	"testing"
)

// #759 (V15-09): ComputeWeightedDigest must not read pooled storage after
// returning it to the sync.Pool. The baseline read pairs[pn-1].v for Max
// AFTER weightedDigestScratchPool.Put — a concurrent owner could refill that
// backing array in between. Two instruments, honestly labeled:
//   - Tier 1 (baseline RED): the race detector flags the write-vs-read pair
//     (append loop vs the Max read) under concurrent callers. This is the
//     audit's own proof instrument. Note make test-short omits -race, so the
//     detector only runs under make test / explicit -race invocations.
//   - Tier 2 (guard): each goroutine uses its OWN distinct constant, so a
//     value stolen from another group's pooled storage reads as that group's
//     constant and is caught (same-value collisions would mask theft —
//     distinct values are deliberate). Deterministic post-fix; NOT sufficient
//     alone as baseline RED (the audit observed no numeric mismatch in ten
//     non-race runs) — the detector is.
func TestComputeWeightedDigest_ConcurrentConstantInputsPreserveInvariants(t *testing.T) {
	const (
		goroutines = 16
		iterations = 250
	)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			// Distinct per-goroutine constant: a cross-group steal is
			// observable as the wrong value. Small independently-owned
			// slices keep the pool cycling fast, maximizing Get/Put overlap.
			sampleVal := int64(100 + g*37)
			values := make([]int64, 8)
			weights := make([]float64, 8)
			// Odd goroutines use uniform non-1.0 weights to exercise the
			// weightedPercentilesFromPairs branch as well.
			w := 1.0
			if g%2 == 1 {
				w = 2.0
			}
			for i := range values {
				values[i] = sampleVal
				weights[i] = w
			}
			for it := 0; it < iterations; it++ {
				d := ComputeWeightedDigest(values, weights)
				if d.Max != sampleVal {
					t.Errorf("goroutine %d iter %d: Max=%d want %d — read another group's pooled storage after Put (#759)", g, it, d.Max, sampleVal)
				}
				if d.P95 != sampleVal {
					t.Errorf("goroutine %d iter %d: P95=%d want %d — percentile corrupted by pooled-storage reuse (#759)", g, it, d.P95, sampleVal)
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestComputeDigest_KnownValues(t *testing.T) {
	d := ComputeDigest([]int64{100, 200, 300})
	if d.Count != 3 {
		t.Fatalf("Count=%d want 3", d.Count)
	}
	if d.Max != 300 {
		t.Fatalf("Max=%d want 300", d.Max)
	}
	if d.P50 != 200 {
		t.Fatalf("P50=%d want 200 (nearest-lower-rank)", d.P50)
	}
}

func TestComputeDigest_Empty(t *testing.T) {
	if d := ComputeDigest(nil); d != (Digest{}) {
		t.Fatalf("empty: %+v", d)
	}
}

func TestWeightedDigestScratch_PairsCapped(t *testing.T) {
	n := 1000
	values := make([]int64, n)
	weights := make([]float64, n)
	for i := range values {
		values[i] = int64(i + 1)
		weights[i] = 1.0
	}

	d := ComputeWeightedDigest(values, weights)
	if d.Count <= 0 {
		t.Fatalf("expected Count > 0, got %d", d.Count)
	}

	scratch := weightedDigestScratchPool.Get().(*weightedDigestScratch)
	defer weightedDigestScratchPool.Put(scratch)

	if cap(scratch.pairs) > maxWeightedPairsCap {
		t.Fatalf("pairs cap should be <= %d after reset, got %d", maxWeightedPairsCap, cap(scratch.pairs))
	}
}

func TestComputeWeightedDigest_AllOnesMatchesUnweighted(t *testing.T) {
	values := []int64{10, 20, 30, 40, 50}
	weights := []float64{1, 1, 1, 1, 1}
	got := ComputeWeightedDigest(append([]int64(nil), values...), weights)
	want := ComputeDigest(append([]int64(nil), values...))
	if got != want {
		t.Fatalf("weighted all-ones %+v != unweighted %+v", got, want)
	}
}
