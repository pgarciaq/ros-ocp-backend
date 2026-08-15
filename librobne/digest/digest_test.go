package digest

import "testing"

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
