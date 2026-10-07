package correlate

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

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

func TestDefaultPolicy_LockedValues(t *testing.T) {
	t.Parallel()

	p := DefaultPolicy()
	assert.Equal(t, 0.30, p.HAbsoluteThresholdS)
	assert.Equal(t, 3.0, p.HBaselineMultiple)
	assert.Equal(t, 1, p.HWindowHours)
	assert.Equal(t, 30, p.HMinDataMinutes)
	assert.Equal(t, 80.0, p.CPUThresholdPct)
	assert.Equal(t, 5, p.SkewToleranceMinutes)
	assert.Equal(t, 2, p.FreshnessHours)
	assert.Equal(t, 24, p.AdvisoryExpiryHours)
	assert.Equal(t, 36, p.NodeFreshnessHours)
}

// TestPolicyFromSettings_MirrorsEngineDefaults guards the two compiled
// default sources against drift: engine HCPCorrelationSettings (settings
// layer) and correlate Policy (evaluator layer) must agree field for field.
// The split exists to avoid an engine<->correlate import cycle.
func TestPolicyFromSettings_MirrorsEngineDefaults(t *testing.T) {
	p := PolicyFromSettings(engine.HCPCorrelationSettings{
		HP99ThresholdS:    0.30,
		HBaselineMultiple: 3,
		CCPUPct:           80,
		CEtcdP99S:         0.01,
		WindowHours:       1,
		SkewMinutes:       5,
		FreshnessHours:    2,
		ExpiryHours:       24,
		ZombieIdleReqPerDay: 100,
		ZombieIdleCPUFloorMC: 10,
		ZombieShortWindowDays:  3,
		ZombieShortRequiredDays: 3,
		ZombieIdleWindowDays:    14,
	})
	assert.Equal(t, DefaultPolicy(), p)
}

func TestEvaluate_VerdictMatrix(t *testing.T) {
	t.Parallel()

	fire := Evaluate(true, true, true, true, false, true, 0.9, 0.3, 0.95, []string{}, "mgmt")
	require.True(t, fire.Fire, "H && C && !N all known must fire")
	assert.Equal(t, "mgmt", fire.ManagementID)

	silent := []struct {
		name                                 string
		hHigh, hKnown, cHigh, cKnown, nP, nK bool
	}{
		{"H && N keeps worker advice", true, true, true, true, true, true},
		{"H without C blames nothing", true, true, false, true, false, true},
		{"unknown H is silence", false, false, true, true, false, true},
		{"unknown C is silence", true, true, false, false, false, true},
		{"unknown N is silence not calm", true, true, true, true, false, false},
		{"nothing known is silence", false, false, false, false, false, false},
		{"at-threshold H is not high", false, true, true, true, false, true},
	}
	for _, tc := range silent {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := Evaluate(tc.hHigh, tc.hKnown, tc.cHigh, tc.cKnown, tc.nP, tc.nK, 0.9, 0.3, 0.95, nil, "mgmt")
			assert.False(t, a.Fire, "must not emit")
		})
	}
}
