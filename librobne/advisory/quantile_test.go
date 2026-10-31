package advisory

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ports of the server's quantile_test.go cases (internal/engine/correlate)
// — the parity contract for the shared histogram math (H leg #723, api-tax
// #724).

func TestQuantile_Matrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		buckets []Bucket
		q       float64
		want    float64
		ok      bool
		delta   float64
	}{
		{
			name:    "uniform p50 lands mid-bucket",
			buckets: []Bucket{{0.1, 10}, {0.2, 20}, {0.3, 30}, {0.4, 40}, {0.5, 50}, {0.6, 60}, {0.7, 70}, {0.8, 80}, {0.9, 90}, {1.0, 100}},
			q:       0.5,
			want:    0.5,
			ok:      true,
		},
		{
			name:    "skewed tail interpolates within top bucket",
			buckets: []Bucket{{0.1, 90}, {1.0, 100}},
			q:       0.99,
			want:    0.91,
			ok:      true,
		},
		{
			name:    "empty is unknown",
			buckets: nil,
			q:       0.99,
			ok:      false,
		},
		{
			name:    "all-zero is unknown not zero pain",
			buckets: []Bucket{{0.1, 0}, {1.0, 0}},
			q:       0.99,
			ok:      false,
		},
		{
			name:    "single bucket interpolates from zero",
			buckets: []Bucket{{0.5, 100}},
			q:       0.99,
			want:    0.495,
			ok:      true,
		},
		{
			name:    "non-monotonic input clamps to running peak",
			buckets: []Bucket{{0.1, 50}, {0.2, 30}, {0.3, 110}},
			q:       0.99,
			want:    0.298167,
			ok:      true,
			delta:   1e-6,
		},
		{
			name:    "infinite tail returns highest finite bound",
			buckets: []Bucket{{0.5, 90}, {math.Inf(1), 100}},
			q:       0.99,
			want:    0.5,
			ok:      true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Quantile(tc.buckets, tc.q)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				tol := tc.delta
				if tol == 0 {
					tol = 1e-9
				}
				assert.InDelta(t, tc.want, got, tol)
			}
		})
	}
}

func TestDeltas_ResetsAndGaps(t *testing.T) {
	t.Parallel()

	prev := []Bucket{{0.1, 100}, {0.5, 200}}
	cur := []Bucket{{0.1, 150}, {0.5, 120}, {1.0, 10}}
	got := Deltas(prev, cur)
	want := map[float64]float64{0.1: 50, 0.5: 120, 1.0: 10}
	require.Len(t, got, 3, "reset bucket resolves to current, missing previous resolves to current")
	for _, b := range got {
		assert.Equal(t, want[b.LE], b.Count, "le %v", b.LE)
	}
	// Duplicate keys collapse to first occurrence.
	dups := Deltas(nil, []Bucket{{0.1, 5}, {0.1, 999}})
	require.Len(t, dups, 1)
	assert.Equal(t, 5.0, dups[0].Count)
}

func TestMedianFloat64(t *testing.T) {
	t.Parallel()

	m, ok := MedianFloat64([]float64{0.03, 0.05, 0.02})
	require.True(t, ok)
	assert.InDelta(t, 0.03, m, 1e-12)
	m, ok = MedianFloat64([]float64{0.02, 0.04})
	require.True(t, ok)
	assert.InDelta(t, 0.03, m, 1e-12)
	_, ok = MedianFloat64(nil)
	assert.False(t, ok)
}
