package engine

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestComputeVMStability(t *testing.T) {
	tests := []struct {
		name        string
		oldVCPU     int32
		newVCPU     int32
		oldMemGiB   int32
		newMemGiB   int32
		want        float32
	}{
		{"no change", 4, 4, 8, 8, 1.0},
		{"50% CPU increase only", 4, 6, 8, 8, 0.75},
		{"50% memory increase only", 4, 4, 8, 12, 0.75},
		{"50% both", 4, 6, 8, 12, 0.5},
		{"100% both", 4, 8, 8, 16, 0.0},
		{"negative variation (decrease)", 8, 4, 16, 8, 0.5},
		{"over 100% clamps to 0", 4, 12, 8, 24, 0.0},
		{"asymmetric", 10, 12, 10, 18, 0.5},
		{"zero old vCPU", 0, 4, 8, 8, 1.0},
		{"zero old memory", 4, 4, 0, 8, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeVMStability(tt.oldVCPU, tt.newVCPU, tt.oldMemGiB, tt.newMemGiB)
			assert.InDelta(t, tt.want, got, 0.01)
		})
	}
}

func TestDetectVMAdoption(t *testing.T) {
	tests := []struct {
		name      string
		curVCPU   int32
		oldVCPU   int32
		curMem    int32
		oldMem    int32
		wantAdopt bool
	}{
		{"exact match", 4, 4, 8, 8, true},
		{"within 5% both", 4, 4, 8, 8, true},
		{"beyond 5% CPU", 6, 4, 8, 8, false},
		{"beyond 5% memory", 4, 4, 12, 8, false},
		{"zero recommended zero actual", 0, 0, 0, 0, true},
		{"zero recommended nonzero actual", 4, 0, 8, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectVMAdoption(tt.curVCPU, tt.oldVCPU, tt.curMem, tt.oldMem)
			assert.Equal(t, tt.wantAdopt, got)
		})
	}
}

func TestCountVMSaturationDays(t *testing.T) {
	tests := []struct {
		name    string
		digests []model.DailyVMDigest
		want    int64
	}{
		{
			"CPU saturated",
			[]model.DailyVMDigest{
				{CPURequestMC: 1000, CPUUsageP95MC: 960, MemRequestKiB: 1000, MemUsageP95KiB: 500},
			},
			1,
		},
		{
			"memory saturated",
			[]model.DailyVMDigest{
				{CPURequestMC: 1000, CPUUsageP95MC: 500, MemRequestKiB: 1000, MemUsageP95KiB: 960},
			},
			1,
		},
		{
			"both saturated counts once",
			[]model.DailyVMDigest{
				{CPURequestMC: 1000, CPUUsageP95MC: 960, MemRequestKiB: 1000, MemUsageP95KiB: 960},
			},
			1,
		},
		{
			"neither saturated",
			[]model.DailyVMDigest{
				{CPURequestMC: 1000, CPUUsageP95MC: 500, MemRequestKiB: 1000, MemUsageP95KiB: 500},
			},
			0,
		},
		{
			"zero request skipped",
			[]model.DailyVMDigest{
				{CPURequestMC: 0, CPUUsageP95MC: 960, MemRequestKiB: 0, MemUsageP95KiB: 960},
			},
			0,
		},
		{
			"empty digests",
			nil,
			0,
		},
		{
			"mixed days",
			[]model.DailyVMDigest{
				{CPURequestMC: 1000, CPUUsageP95MC: 960, MemRequestKiB: 1000, MemUsageP95KiB: 500},
				{CPURequestMC: 1000, CPUUsageP95MC: 500, MemRequestKiB: 1000, MemUsageP95KiB: 500},
				{CPURequestMC: 1000, CPUUsageP95MC: 500, MemRequestKiB: 1000, MemUsageP95KiB: 960},
			},
			2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountVMSaturationDays(tt.digests)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildVMQualityRows(t *testing.T) {
	clusterUUID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	t.Run("empty recs returns nil", func(t *testing.T) {
		rows := BuildVMQualityRows(nil, nil, nil)
		assert.Nil(t, rows)
	})

	t.Run("basic quality row generation", func(t *testing.T) {
		recs := []model.VMRecommendation{
			{
				OrgID:                "org1",
				ClusterUUID:          clusterUUID,
				Namespace:            "ns1",
				VMName:               "vm-1",
				Engine:               "cost",
				RecommendedVCPU:      4,
				RecommendedMemoryGiB: 8,
				CurrentVCPU:          4,
				CurrentMemoryGiB:     8,
			},
		}
		rows := BuildVMQualityRows(recs, nil, nil)
		assert.Len(t, rows, 1)
		assert.Equal(t, "ns1", rows[0].Namespace)
		assert.Equal(t, "vm-1", rows[0].VMName)
		assert.Equal(t, "cost", rows[0].Engine)
		assert.Equal(t, float32(1.0), rows[0].StabilityPct)
		assert.False(t, rows[0].AdoptionDetected)
	})

	t.Run("deduplicates by namespace+vm+engine", func(t *testing.T) {
		recs := []model.VMRecommendation{
			{OrgID: "org1", ClusterUUID: clusterUUID, Namespace: "ns1", VMName: "vm-1", Engine: "cost", RecommendedVCPU: 4, RecommendedMemoryGiB: 8},
			{OrgID: "org1", ClusterUUID: clusterUUID, Namespace: "ns1", VMName: "vm-1", Engine: "cost", RecommendedVCPU: 8, RecommendedMemoryGiB: 16},
		}
		rows := BuildVMQualityRows(recs, nil, nil)
		assert.Len(t, rows, 1)
	})

	t.Run("filters non-cost/performance engines", func(t *testing.T) {
		recs := []model.VMRecommendation{
			{OrgID: "org1", ClusterUUID: clusterUUID, Namespace: "ns1", VMName: "vm-1", Engine: "unknown", RecommendedVCPU: 4, RecommendedMemoryGiB: 8},
		}
		rows := BuildVMQualityRows(recs, nil, nil)
		assert.Len(t, rows, 0)
	})
}

func TestComputeInt32Variation(t *testing.T) {
	tests := []struct {
		name    string
		current int32
		rec     int32
		want    float64
	}{
		{"no change", 100, 100, 0},
		{"50% increase", 100, 150, 50},
		{"50% decrease", 100, 50, -50},
		{"100% increase", 100, 200, 100},
		{"zero current returns 0", 0, 100, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeInt32Variation(tt.current, tt.rec)
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}
