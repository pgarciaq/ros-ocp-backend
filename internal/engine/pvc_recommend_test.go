package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComputePVCRecommendation_Orphaned(t *testing.T) {
	digests := make([]PVCDigestRow, 5)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "old-data-pvc",
			PV:            "pv-001",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30, // 100 GiB
			UsageBytesMin: 0,
			UsageBytesMax: 0,
			UsageBytesAvg: 0,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid")

	assert.Equal(t, PVCRecTypeOrphaned, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, NotifPVCOrphaned)
	assert.Equal(t, int64(0), rec.UsageBytesMax)
	assert.Equal(t, float64(0), rec.UsageRatio)
	assert.Equal(t, 5, rec.DataDays)
}

func TestComputePVCRecommendation_Oversized(t *testing.T) {
	digests := make([]PVCDigestRow, 7)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "staging",
			PVC:           "app-logs",
			PV:            "pv-002",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30, // 100 GiB
			UsageBytesMin: 5 << 30,   // 5 GiB
			UsageBytesMax: 10 << 30,  // 10 GiB (10% usage)
			UsageBytesAvg: 7 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid")

	assert.Equal(t, PVCRecTypeOversized, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, NotifPVCOversized)
	assert.NotNil(t, rec.RecommendedBytes)
	// Recommended should be 2x max usage = 20 GiB
	assert.Equal(t, int64(20<<30), *rec.RecommendedBytes)
	assert.InDelta(t, 0.10, rec.UsageRatio, 0.01)
}

func TestComputePVCRecommendation_NearFull(t *testing.T) {
	digests := make([]PVCDigestRow, 3)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "db-data",
			PV:            "pv-003",
			StorageClass:  "io2",
			CapacityBytes: 50 << 30, // 50 GiB
			UsageBytesMin: 40 << 30,
			UsageBytesMax: 45 << 30, // 90% usage
			UsageBytesAvg: 42 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid")

	assert.Equal(t, PVCRecTypeNearFull, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, NotifPVCNearFull)
	assert.NotNil(t, rec.RecommendedBytes)
	assert.InDelta(t, 0.90, rec.UsageRatio, 0.01)
}

func TestComputePVCRecommendation_Healthy(t *testing.T) {
	digests := make([]PVCDigestRow, 5)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "app-data",
			PV:            "pv-004",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30, // 100 GiB
			UsageBytesMin: 30 << 30,
			UsageBytesMax: 50 << 30, // 50% usage — healthy
			UsageBytesAvg: 40 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid")

	assert.Equal(t, PVCRecTypeHealthy, rec.RecommendationType)
	assert.Empty(t, rec.NotificationCodes)
}

func TestComputePVCRecommendation_GrowthTrend(t *testing.T) {
	// Simulate linear growth: 10 GiB at day 0, growing 1 GiB/day
	digests := make([]PVCDigestRow, 10)
	for i := range digests {
		usage := int64((10 + i) << 30)
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "growing-pvc",
			PV:            "pv-005",
			StorageClass:  "gp3",
			CapacityBytes: 50 << 30, // 50 GiB
			UsageBytesMin: usage - (1 << 29),
			UsageBytesMax: usage,
			UsageBytesAvg: usage,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid")

	// Growth should be ~1 GiB/day
	assert.InDelta(t, float64(1<<30), float64(rec.GrowthBytesPerDay), float64(1<<28))
	// Days to full: (50 GiB - 19 GiB max) / 1 GiB per day ≈ 31
	assert.NotNil(t, rec.DaysToFull)
	assert.InDelta(t, 31, *rec.DaysToFull, 5)
}

func TestComputePVCRecommendation_InsufficientData(t *testing.T) {
	// Only 2 days of zero usage — should NOT be classified as orphaned
	digests := []PVCDigestRow{
		{
			BucketDate:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "new-pvc",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
		},
		{
			BucketDate:    time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "new-pvc",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
		},
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid")

	// Not enough data to classify as orphaned (< pvcMinDataDays)
	assert.Equal(t, PVCRecTypeHealthy, rec.RecommendationType)
}

func TestComputePVCGrowthSlope(t *testing.T) {
	// Perfect linear growth: 0, 10, 20, 30, 40 bytes per day
	digests := []PVCDigestRow{
		{UsageBytesAvg: 0},
		{UsageBytesAvg: 10},
		{UsageBytesAvg: 20},
		{UsageBytesAvg: 30},
		{UsageBytesAvg: 40},
	}

	slope := computePVCGrowthSlope(digests)
	assert.InDelta(t, 10.0, slope, 0.001)
}

func TestComputePVCGrowthSlope_Flat(t *testing.T) {
	digests := []PVCDigestRow{
		{UsageBytesAvg: 100},
		{UsageBytesAvg: 100},
		{UsageBytesAvg: 100},
	}

	slope := computePVCGrowthSlope(digests)
	assert.InDelta(t, 0.0, slope, 0.001)
}

func TestComputePVCGrowthSlope_InsufficientData(t *testing.T) {
	digests := []PVCDigestRow{{UsageBytesAvg: 50}}
	slope := computePVCGrowthSlope(digests)
	assert.Equal(t, 0.0, slope)
}
