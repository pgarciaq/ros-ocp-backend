package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputePVCStability(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }

	tests := []struct {
		name     string
		oldBytes *int64
		newBytes *int64
		want     float32
	}{
		{"no change", i64(1000), i64(1000), 1.0},
		{"nil old returns 1.0", nil, i64(1000), 1.0},
		{"nil new returns 1.0", i64(1000), nil, 1.0},
		{"both nil returns 1.0", nil, nil, 1.0},
		{"50% increase", i64(1000), i64(1500), 0.5},
		{"100% increase", i64(1000), i64(2000), 0.0},
		{"50% decrease", i64(1000), i64(500), 0.5},
		{"10% change", i64(1000), i64(1100), 0.9},
		{"200% increase clamps to 0", i64(1000), i64(3000), 0.0},
		{"zero old returns 1.0", i64(0), i64(1000), 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputePVCStability(tt.oldBytes, tt.newBytes)
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}

func TestDetectPVCAdoption(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }

	tests := []struct {
		name      string
		capacity  int64
		oldRec    *int64
		wantAdopt bool
	}{
		{"exact match", 1000, i64(1000), true},
		{"within 5%", 1040, i64(1000), true},
		{"at 5% boundary", 1050, i64(1000), true},
		{"beyond 5%", 1060, i64(1000), false},
		{"much larger capacity", 2000, i64(1000), false},
		{"nil old rec", 1000, nil, false},
		{"zero capacity, zero rec", 0, i64(0), true},
		{"zero capacity, nonzero rec", 0, i64(1000), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPVCAdoption(tt.capacity, tt.oldRec)
			assert.Equal(t, tt.wantAdopt, got)
		})
	}
}

func TestCountPVCDaysAboveThreshold(t *testing.T) {
	tests := []struct {
		name      string
		digests   []PVCDigestRow
		threshold float64
		want      int64
	}{
		{
			"all above 95%",
			[]PVCDigestRow{
				{UsageBytesMax: 960, CapacityBytes: 1000},
				{UsageBytesMax: 980, CapacityBytes: 1000},
			},
			0.95,
			2,
		},
		{
			"none above 95%",
			[]PVCDigestRow{
				{UsageBytesMax: 500, CapacityBytes: 1000},
				{UsageBytesMax: 900, CapacityBytes: 1000},
			},
			0.95,
			0,
		},
		{
			"one above, one below",
			[]PVCDigestRow{
				{UsageBytesMax: 960, CapacityBytes: 1000},
				{UsageBytesMax: 940, CapacityBytes: 1000},
			},
			0.95,
			1,
		},
		{
			"zero capacity skipped",
			[]PVCDigestRow{
				{UsageBytesMax: 960, CapacityBytes: 0},
			},
			0.95,
			0,
		},
		{
			"empty digests",
			nil,
			0.95,
			0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountPVCDaysAboveThreshold(tt.digests, tt.threshold)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildPVCQualityRows(t *testing.T) {
	t.Run("empty recs returns nil", func(t *testing.T) {
		rows := BuildPVCQualityRows(nil, nil, nil)
		assert.Nil(t, rows)
	})

	t.Run("basic quality row generation", func(t *testing.T) {
		i64 := func(v int64) *int64 { return &v }
		recs := []PVCRec{
			{
				OrgID:            "org1",
				ClusterUUID:      "cluster-1",
				Namespace:        "ns1",
				PVC:              "pvc-1",
				CapacityBytes:    1000,
				RecommendedBytes: i64(1100),
			},
		}
		rows := BuildPVCQualityRows(recs, nil, nil)
		assert.Len(t, rows, 1)
		assert.Equal(t, "ns1", rows[0].Namespace)
		assert.Equal(t, "pvc-1", rows[0].PVCName)
		assert.Equal(t, "cost", rows[0].Engine)
		assert.Equal(t, float32(1.0), rows[0].StabilityPct)
		assert.False(t, rows[0].AdoptionDetected)
	})

	t.Run("deduplicates by namespace+pvc", func(t *testing.T) {
		i64 := func(v int64) *int64 { return &v }
		recs := []PVCRec{
			{OrgID: "org1", ClusterUUID: "c1", Namespace: "ns1", PVC: "pvc-1", RecommendedBytes: i64(100)},
			{OrgID: "org1", ClusterUUID: "c1", Namespace: "ns1", PVC: "pvc-1", RecommendedBytes: i64(200)},
		}
		rows := BuildPVCQualityRows(recs, nil, nil)
		assert.Len(t, rows, 1)
	})
}
